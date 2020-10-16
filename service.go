package remindme

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/datastore"
	"github.com/darrenmcc/dizmo"
	"github.com/go-kit/kit/endpoint"
	kittransport "github.com/go-kit/kit/transport/http"
	"github.com/gorilla/mux"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

const reminderKind = "reminder"

var eastern, _ = time.LoadLocation("America/New_York")

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
	Created int64
}

type service struct {
	ds       *datastore.Client
	sendgrid *sendgrid.Client
	to       *mail.Email
	from     *mail.Email
	secret   string
}

func NewService(to, from, secret, sgSecret string) (dizmo.Service, error) {
	ctx := context.Background()
	ds, err := datastore.NewClient(ctx, dizmo.GoogleProjectID())
	if err != nil {
		return nil, err
	}

	return &service{
		ds:       ds,
		sendgrid: sendgrid.NewSendClient(sgSecret),
		secret:   secret,
		to:       mail.NewEmail(to, to),
		from:     mail.NewEmail("RemindMe", from),
	}, nil
}

func (s *service) HTTPEndpoints() map[string]map[string]dizmo.HTTPEndpoint {
	return map[string]map[string]dizmo.HTTPEndpoint{
		"/": {
			"GET": {
				Decoder:  s.authDecoder,
				Endpoint: s.Home,
				Encoder:  s.HomeEncoder,
			},
		},
		"/new": {
			"POST": {
				Decoder:  s.newDecoder,
				Endpoint: s.New,
			},
		},
		"/new-form": {
			"POST": {
				Decoder:  s.newFormDecoder,
				Endpoint: s.New,
			},
		},
		"/remindme": {
			"GET": {
				Decoder:  s.authDecoder,
				Endpoint: s.RemindMe,
			},
		},
		"/{id:[0-9]+}": {
			"DELETE": {
				Decoder:  s.deleteDecoder,
				Endpoint: s.Delete,
			},
		},
	}
}

func (s *service) deleteDecoder(ctx context.Context, r *http.Request) (interface{}, error) {
	_, err := s.authDecoder(ctx, r)
	if err != nil {
		return nil, err
	}
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		return nil, dizmo.NewErrorStatusResponse(err.Error(), http.StatusInternalServerError)
	}
	return id, nil
}

func (s *service) Delete(ctx context.Context, req interface{}) (interface{}, error) {
	id := req.(int64)
	var data reminderData
	_, err := s.ds.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		k := datastore.IDKey(reminderKind, id, nil)
		err := tx.Get(k, &data)
		if err != nil {
			return err
		}
		data.Year = 1991 // just set the year far in the past to "delete"
		_, err = tx.Put(k, &data)
		return err
	})
	if err != nil {
		if err == datastore.ErrNoSuchEntity {
			return nil, dizmo.NewErrorStatusResponse(fmt.Sprintf("no reminder with id=%d exists", id), http.StatusBadRequest)
		}
		return nil, dizmo.NewErrorStatusResponse(err.Error(), http.StatusInternalServerError)
	}
	return dizmo.NewJSONStatusResponse(fmt.Sprintf("reminder '%s' deleted", data.Message), http.StatusOK), nil
}

func (s *service) authDecoder(ctx context.Context, r *http.Request) (interface{}, error) {
	sec := r.URL.Query().Get("sec")
	if sec != s.secret {
		dizmo.Errorf(ctx, "incorrect secret: '%s'", sec)
		return nil, dizmo.NewErrorStatusResponse("no way josé", http.StatusUnauthorized)
	}
	return nil, nil
}

func (s *service) newFormDecoder(ctx context.Context, r *http.Request) (interface{}, error) {
	_, err := s.authDecoder(ctx, r)
	if err != nil {
		return nil, err
	}
	return reminder{
		Message: r.PostFormValue("message"),
		Date:    r.PostFormValue("date"),
		Repeat:  r.PostFormValue("repeat") == "true",
	}, nil
}

func (s *service) newDecoder(ctx context.Context, r *http.Request) (interface{}, error) {
	_, err := s.authDecoder(ctx, r)
	if err != nil {
		return nil, err
	}

	var reminder reminder
	err = json.NewDecoder(r.Body).Decode(&reminder)
	if err != nil {
		b, _ := httputil.DumpRequest(r, true)
		dizmo.Errorf(ctx, "unable to unmarshal json: %s\n%s", err, b)
		return nil, dizmo.NewErrorStatusResponse(err.Error(), http.StatusInternalServerError)
	}
	return reminder, nil
}

func (s *service) New(ctx context.Context, req interface{}) (interface{}, error) {
	reminder := req.(reminder)

	k, err := s.ds.Put(ctx, datastore.IncompleteKey(reminderKind, nil), reminderToData(reminder))
	if err != nil {
		dizmo.Errorf(ctx, "unable to put reminder: %s", err)
		return nil, dizmo.NewErrorStatusResponse("unable to put reminder: "+err.Error(), http.StatusInternalServerError)
	}

	rType := "instant"
	if reminder.Repeat {
		rType = "repeating"
	}
	resp := fmt.Sprintf("created %s reminder '%s' for %s [id=%d]",
		rType, reminder.Message, reminder.Date, k.ID)
	dizmo.Debugf(ctx, resp)
	return dizmo.NewJSONStatusResponse(resp, http.StatusCreated), nil
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
		Created: time.Now().Unix(),
	}
}

func (s *service) RemindMe(ctx context.Context, req interface{}) (interface{}, error) {
	var (
		mtx     sync.Mutex
		results []*reminderData

		now     = time.Now().In(eastern)
		y, m, d = now.Year(), int(now.Month()), now.Day()
	)

	// get reminders for this year
	var errg errgroup.Group
	errg.Go(func() error {
		var data []*reminderData
		q := datastore.NewQuery(reminderKind).
			Filter("Month =", m).
			Filter("Day =", d).
			Filter("Year =", y)
		_, err := s.ds.GetAll(ctx, q, &data)
		if err != nil {
			return fmt.Errorf("unable to query for this day's reminders: %w", err)
		}
		mtx.Lock()
		defer mtx.Unlock()
		for _, reminder := range data {
			results = append(results, reminder)
		}
		return nil
	})

	// get repeating reminders
	errg.Go(func() error {
		var data []*reminderData
		q := datastore.NewQuery(reminderKind).
			Filter("Month =", m).
			Filter("Day =", d).
			Filter("Year =", 0) // zero denotes a yearly repeating reminder
		_, err := s.ds.GetAll(ctx, q, &data)
		if err != nil {
			return fmt.Errorf("unable to query for repeating reminders: %w", err)
		}
		mtx.Lock()
		defer mtx.Unlock()
		for _, reminder := range data {
			results = append(results, reminder)
		}
		return nil
	})

	err := errg.Wait()
	if err != nil {
		dizmo.Errorf(ctx, err.Error())
		return nil, dizmo.NewErrorStatusResponse("unable to get reminders: "+err.Error(), http.StatusInternalServerError)
	}

	if len(results) == 0 {
		// no reminders for today, exit
		dizmo.Infof(ctx, "no reminders today")
		return dizmo.NewErrorStatusResponse("no reminders today", http.StatusOK), nil
	}

	var plural string
	if len(results) > 1 {
		// pluralize 'reminders'
		plural = "s"
	}

	dizmo.Infof(ctx, "found %d reminder%s", len(results), plural)

	body := `<ol type="1">`
	for _, r := range results {
		line := fmt.Sprintf("<li>%s</li>", r.Message)
		dizmo.Infof(ctx, line)
		body += line
	}
	body += `</ol>`

	eml := mail.NewSingleEmail(
		// TDOD: consider using another email so gmail doesn't think it's spam
		s.from,
		fmt.Sprintf("You have %d reminder%s for %s",
			len(results),
			plural,
			now.Format("Monday Jan 02, 2006")),
		s.to,
		"XXX", // just can't be empty, screw plaintext emails apparently
		body)
	response, err := s.sendgrid.Send(eml)
	if err != nil {
		dizmo.Errorf(ctx, "unable to send email: %s, sendgrid response: %#v", err, response)
		return nil, dizmo.NewErrorStatusResponse("unable to send email: "+err.Error(), http.StatusInternalServerError)
	}

	if response != nil && response.StatusCode != http.StatusAccepted {
		dizmo.Errorf(ctx, "sendgrid response: %#v", response)
	}

	return dizmo.NewJSONStatusResponse(response, http.StatusOK), nil
}

// these methods fullfill dizmos "Service" interface but aren't being used yet.
func (s *service) Middleware(e endpoint.Endpoint) endpoint.Endpoint { return e }
func (s *service) HTTPMiddleware(h http.Handler) http.Handler       { return h }
func (s *service) HTTPOptions() []kittransport.ServerOption         { return nil }
func (s *service) HTTPRouterOptions() []dizmo.RouterOption          { return nil }
func (s *service) RPCMiddleware() grpc.UnaryServerInterceptor       { return nil }
func (s *service) RPCOptions() []grpc.ServerOption                  { return nil }
func (s *service) RPCServiceDesc() *grpc.ServiceDesc                { return nil }
