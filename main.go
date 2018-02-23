package remindme

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"
	"google.golang.org/appengine/mail"
)

const reminderKind = "reminder"

type reminder struct {
	Message string `json:"message"`
	Date    string `json:"date"`
	Repeat  bool   `json:"repeat"`
}

type reminderData struct {
	Message string
	Month   int
	Day     int
	Year    int
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

	var reminder reminder
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
	fmt.Fprintf(w, "created reminder: '%s' for %s (repeat=%t)",
		reminder.Message, reminder.Date, reminder.Repeat)
}

func reminderToData(r reminder) *reminderData {
	tokens := strings.Split(r.Date, "-")
	var year int
	if !r.Repeat {
		year, _ = strconv.Atoi(tokens[0])
	}
	month, _ := strconv.Atoi(tokens[1])
	day, _ := strconv.Atoi(tokens[2])

	return &reminderData{
		Message: r.Message,
		Year:    year,
		Month:   month,
		Day:     day,
	}
}

func remind(w http.ResponseWriter, r *http.Request) {
	var (
		now     = time.Now().In(loc)
		y, m, d = now.Year(), now.Month(), now.Day()

		errg, ctx    = errgroup.WithContext(appengine.NewContext(r))
		reminderChan = make(chan *reminderData)
	)

	// get reminders for this year
	errg.Go(func() error {
		var data []*reminderData
		_, err := datastore.NewQuery(reminderKind).
			Filter("Month =", m).
			Filter("Day =", d).
			Filter("Year =", y).
			GetAll(ctx, &data)
		for _, reminder := range data {
			reminderChan <- reminder
		}
		return err
	})

	// get repeating reminders
	errg.Go(func() error {
		var data []*reminderData
		_, err := datastore.NewQuery(reminderKind).
			Filter("Month =", m).
			Filter("Day =", d).
			Filter("Year =", 0).
			GetAll(ctx, &data)
		for _, reminder := range data {
			reminderChan <- reminder
		}
		return err
	})

	// collect reminders
	var reminders []*reminderData
	go func() {
		for reminder := range reminderChan {
			reminders = append(reminders, reminder)
		}
	}()

	err := errg.Wait()
	if err != nil {
		log.Errorf(ctx, "unable to get reminders: %s", err)
		http.Error(w, "unable to get reminders", http.StatusInternalServerError)
		return
	}

	var s string
	switch {
	case len(reminders) == 0:
		// no reminders for today, exit
		w.WriteHeader(http.StatusOK)
		return
	case len(reminders) > 1:
		// pluralize 'reminders'
		s = "s"
	}

	var body string
	for i, r := range reminders {
		body += fmt.Sprintf("%d. %s\n", i+1, r.Message)
	}

	// send the email
	if err := mail.Send(ctx, &mail.Message{
		Sender: "RemindMe <remindme@darren-reminder.appspotmail.com>",
		To:     []string{email},
		Subject: fmt.Sprintf("You have %d reminder%s for %s",
			len(reminders), s, time.Now().Format("Monday Jan 02, 2006")),
		Body: body,
	}); err != nil {
		log.Errorf(ctx, "unable to send email: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		panic("unable to find '" + k + "' in env")
	}
	return v
}
