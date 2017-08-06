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
	email  string
	secret string
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

	http.HandleFunc("/remindme", remindme)
	http.HandleFunc("/new", new)
}

func new(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
	if r.Method != "POST" {
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
		http.Error(w, "nunable to unmarshal json", http.StatusInternalServerError)
		return
	}

	key := datastore.NewKey(ctx, reminderKind, "", time.Now().UnixNano(), nil)
	_, err = datastore.Put(ctx, key, reminderToData(&reminder))
	if err != nil {
		log.Errorf(ctx, "unable to put reminder: %s", err)
		http.Error(w, "nunable to put reminder", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Write([]byte("success"))
}

func reminderToData(r *Reminder) *ReminderData {
	tokens := strings.Split(r.Date, "-")
	month, _ := strconv.Atoi(tokens[1])
	day, _ := strconv.Atoi(tokens[2])
	var year int
	if !r.Repeat {
		year, _ = strconv.Atoi(tokens[0])
	}
	return &ReminderData{
		Message: r.Message,
		Month:   month,
		Day:     day,
		Year:    year,
		Created: time.Now().Unix(),
	}
}

func remindme(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)

	// get reminders from datastore
	now := time.Now()
	var reminders []ReminderData
	_, err := datastore.NewQuery(reminderKind).
		Filter("Month =", now.Month()).
		Filter("Day =", now.Day()).GetAll(ctx, &reminders)
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
			Sender: fmt.Sprintf("RemindMe <%s>", email),
			To:     []string{email},
			Subject: fmt.Sprintf("You have %d reminder%s for %s",
				n, s, time.Now().Format("02-Jan-06")),
			Body: strings.Join(messages, "\n"),
		})
		if err != nil {
			log.Errorf(ctx, "unable to send email: %s", err)
			http.Error(w, "unable to send email", http.StatusInternalServerError)
		}
	}

	w.WriteHeader(http.StatusOK)
}
