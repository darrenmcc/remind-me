package remindme

import (
	"context"
	"net/http"
	"text/template"
	"time"
)

func (s *service) Home(ctx context.Context, req interface{}) (interface{}, error) {
	return nil, nil
}

func (s *service) HomeEncoder(ctx context.Context, w http.ResponseWriter, _ interface{}) error {
	now := time.Now().In(eastern)
	if now.Hour() >= 6 {
		now = now.AddDate(0, 0, 1)
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Add("Content-Type", "text/html; charset=utf-8")
	return form.Execute(w, struct {
		MinDate string
		Secret  string
	}{
		MinDate: now.Format("2006-01-02"),
		Secret:  s.secret,
	})
}

var form = func() *template.Template {
	t, err := template.New("homepage").Parse(`
		<!doctype html>
		<html lang="en">
		<head>
			<!-- Required meta tags -->
			<meta charset="utf-8">
			<meta name="viewport" content="width=device-width, initial-scale=1, shrink-to-fit=no">
			<!-- Bootstrap CSS -->
			<link rel="stylesheet" href="https://maxcdn.bootstrapcdn.com/bootstrap/4.0.0/css/bootstrap.min.css" integrity="sha384-Gn5384xqQ1aoWXA+058RXPxPg6fy4IWvTNh0E263XmFcJlSAwiGgFAW/dAiS6JXm" crossorigin="anonymous">
		</head>
		<body>
			<form action="/new-form?sec={{ .Secret }}" method="post">
				<div class="form-group">
					<label for="message">Reminder</label>
					<textarea class="form-control" id="message" name="message" rows="3"></textarea>
				</div>
				<div class="form-group">
					<input type="date" id="date" name="date" value="{{ .MinDate }}" min="{{ .MinDate }}">
				</div>
				<div>
					<input type="checkbox" id="repeat" name="repeat" value="true">
  					<label for="repeat"> Repeat</label><br>
				</div>
				<button type="submit" class="btn btn-primary">Save</button>
			</form>
			<!-- Optional JavaScript -->
			<!-- jQuery first, then Popper.js, then Bootstrap JS -->
			<script src="https://code.jquery.com/jquery-3.2.1.slim.min.js" integrity="sha384-KJ3o2DKtIkvYIK3UENzmM7KCkRr/rE9/Qpg6aAZGJwFDMVNA/GpGFF93hXpG5KkN" crossorigin="anonymous"></script>
			<script src="https://cdnjs.cloudflare.com/ajax/libs/popper.js/1.12.9/umd/popper.min.js" integrity="sha384-ApNbgh9B+Y1QKtv3Rn7W3mgPxhU9K/ScQsAP7hUibX39j7fakFPskvXusvfa0b4Q" crossorigin="anonymous"></script>
			<script src="https://maxcdn.bootstrapcdn.com/bootstrap/4.0.0/js/bootstrap.min.js" integrity="sha384-JZR6Spejh4U02d8jOt6vLEHfe/JQGiRRSQQxSfFWpi1MquVdAyjUar5+76PVCmYl" crossorigin="anonymous"></script>
		</body>
		</html>`)
	if err != nil {
		panic(err)
	}
	return t
}()
