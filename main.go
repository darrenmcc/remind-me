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

const (
	reminderKind = "reminder"
	dateFmt      = "Monday Jan 02, 2006"

	maxSubjectLength  = 78
	baseSubjectLength = 51
)

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

	eml := mail.Message{
		Sender: "RemindMe <remindme@darren-reminder.appspotmail.com>",
		To:     []string{email},
	}

	switch {
	// we have no reminds for today, exit
	case len(messages) == 0:
		w.WriteHeader(http.StatusOK)
		return
	// if we only have 1 reminder and it's short enough to fit in the subject line
	case len(messages) == 1 && len(messages[0]) <= maxSubjectLength-baseSubjectLength:
		eml.Subject = fmt.Sprintf("You have 1 reminder for %s: '%s' EOM",
			time.Now().Format("Monday Jan 02, 2006"),
			messages[0])
	// multiple reminders or 1 long one, enumerate them in the body
	default:
		eml.Subject = fmt.Sprintf("You have %d reminders for %s",
			len(messages),
			time.Now().Format(dateFmt))
		eml.Body = enumerateMessages(messages)
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

func enumerateMessages(messages []string) (body string) {
	for i, msg := range messages {
		body += fmt.Sprintf("%d. %s\n", i+1, msg)
	}
	return body
}
