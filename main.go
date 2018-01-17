package remindme

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/mail"
)

const reminderKind = "reminder"

type Reminder struct {
	Message string `json:"message"`
	Date    string `json:"date"`
	Repeat  bool   `json:"repeat"`
}

type ReminderData struct {
	Message string
	Month   int
	Day     int
	Year    int
	Created int64
}

var (
	email  string
	secret string

	loc, _ = time.LoadLocation("America/New_York")
)

func init() {
	var ok bool
	email, ok = os.LookupEnv("EMAIL")
	if !ok {
		panic("unable to find email in env")
	}
	secret, ok = os.LookupEnv("SECRET")
	if !ok {
		panic("unable to find secret in env")
	}

	http.HandleFunc("/remindme", remind)
	http.HandleFunc("/new", newReminder)
}

// listReminders will list the next n remindeds.
func listReminders(w http.ResponseWriter, r *http.Request) {
	// ctx := appengine.NewContext(r)
	// now := time.Now().In(loc)
	// oneMo := time.Now().In(loc).AddDate(0, 1, 0)

	// datastore.NewQuery(reminderKind).
}

func newReminder(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
	if r.Method != http.MethodPost {
		log.Errorf(ctx, "not a post")
		http.Error(w, "not a post", http.StatusBadRequest)
		return
	}

	s := r.URL.Query().Get("sec")
	if s != secret {
		log.Errorf(ctx, "incorrect secret: '%s'", s)
		http.Error(w, "no way josé", http.StatusUnauthorized)
		return
	}

	var reminder Reminder
	err := json.NewDecoder(r.Body).Decode(&reminder)
	if err != nil {
		log.Errorf(ctx, "unable to unmarshal json: %s", err)
		http.Error(w, "unable to unmarshal json", http.StatusInternalServerError)
		return
	}

	key := datastore.NewKey(ctx, reminderKind, "", time.Now().UnixNano(), nil)
	_, err = datastore.Put(ctx, key, reminderToData(reminder))
	if err != nil {
		log.Errorf(ctx, "unable to put reminder: %s", err)
		http.Error(w, "unable to put reminder", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	io.WriteString(w, "created reminder: "+reminder.Message)
}

func reminderToData(r Reminder) *ReminderData {
	tokens := strings.Split(r.Date, "-")
	var year int
	if !r.Repeat {
		year, _ = strconv.Atoi(tokens[0])
	}
	month, _ := strconv.Atoi(tokens[1])
	day, _ := strconv.Atoi(tokens[2])

	return &ReminderData{
		Message: r.Message,
		Year:    year,
		Month:   month,
		Day:     day,
		Created: time.Now().Unix(),
	}
}

func remind(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)

	// get reminders from datastore
	now := time.Now().In(loc)
	var reminders []ReminderData
	_, err := datastore.NewQuery(reminderKind).
		Filter("Month =", now.Month()).
		Filter("Day =", now.Day()).
		GetAll(ctx, &reminders)
	if err != nil {
		log.Errorf(ctx, "unable to get reminders: %s", err)
		http.Error(w, "unable to get reminders", http.StatusInternalServerError)
		return
	}

	// filter messages we don't want to send
	var messages []string
	for _, r := range reminders {
		if r.Year != 0 && r.Year != now.Year() {
			continue
		}
		log.Infof(ctx, r.Message)
		messages = append(messages, r.Message)
	}

	n := len(messages)
	if n > 0 {
		var s string
		if n > 1 {
			s = "s"
		}

		// send the email
		err = mail.Send(ctx, &mail.Message{
			Sender: "RemindMe <remindme@darren-reminder.appspotmail.com>",
			To:     []string{email},
			Subject: fmt.Sprintf("You have %d reminder%s for %s",
				n, s, time.Now().Format("02-Jan-06")),
			Body: enumerateMessages(messages),
		})
		if err != nil {
			log.Errorf(ctx, "unable to send email: %s", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}

func enumerateMessages(messages []string) (body string) {
	for i, msg := range messages {
		body += fmt.Sprintf("%d. %s\n", i+1, msg)
	}
	return body
}
