package remindme

import (
	"encoding/json"
	"fmt"
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
	email  = mustEnv("EMAIL")
	secret = mustEnv("SECRET")

	loc, _ = time.LoadLocation("America/New_York")
)

func init() {
	http.HandleFunc("/remindme", remind)
	http.HandleFunc("/new", newReminder)
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
	fmt.Fprintf(w, "created reminder: '%s' for %d (repeat=%t)",
		reminder.Message, reminder.Date, reminder.Repeat)
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
		if r.Year == 0 || r.Year == now.Year() {
			log.Infof(ctx, r.Message)
			messages = append(messages, r.Message)
		}
	}

	eml := mail.Message{
		Sender: "RemindMe <remindme@darren-reminder.appspotmail.com>",
		To:     []string{email},
	}

	var s string

	switch {
	// we have no reminds for today, exit
	case len(messages) == 0:
		w.WriteHeader(http.StatusOK)
		return

	case len(messages) > 1:
		s = "s"
		fallthrough

	// multiple reminders, enumerate them in the body
	default:
		eml.Subject = fmt.Sprintf("You have %d reminder%s for %s",
			len(messages),
			s,
			time.Now().Format("Monday Jan 02, 2006"))
		for i, msg := range messages {
			eml.Body += fmt.Sprintf("%d. %s\n", i+1, msg)
		}
	}

	// send the email
	err = mail.Send(ctx, &eml)
	if err != nil {
		log.Errorf(ctx, "unable to send email: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	return
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		panic("unable to find '" + k + "' in env")
	}
	return v
}
