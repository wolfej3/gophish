package sms

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewUnsupportedProvider(t *testing.T) {
	if _, err := New("nexmo", "sid", "token"); err != ErrUnsupportedProvider {
		t.Fatalf("expected ErrUnsupportedProvider, got %v", err)
	}
}

func TestTwilioSendRequestShape(t *testing.T) {
	var gotPath, gotTo, gotFrom, gotBody, gotUser, gotPass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotUser, gotPass, _ = r.BasicAuth()
		r.ParseForm()
		gotTo = r.PostForm.Get("To")
		gotFrom = r.PostForm.Get("From")
		gotBody = r.PostForm.Get("Body")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"sid":"SM123","status":"queued"}`))
	}))
	defer srv.Close()
	t.Setenv("GOPHISH_SMS_BASE_URL", srv.URL)

	sender, err := New("twilio", "AC_sid", "auth_token")
	if err != nil {
		t.Fatal(err)
	}
	err = sender.Send(context.Background(), Message{To: "+15551230000", From: "+15559999999", Body: "hello http://x/l/abc"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if !strings.Contains(gotPath, "/2010-04-01/Accounts/AC_sid/Messages.json") {
		t.Errorf("path = %q", gotPath)
	}
	if gotUser != "AC_sid" || gotPass != "auth_token" {
		t.Errorf("basic auth = %q:%q", gotUser, gotPass)
	}
	if gotTo != "+15551230000" || gotFrom != "+15559999999" || gotBody != "hello http://x/l/abc" {
		t.Errorf("form wrong: to=%q from=%q body=%q", gotTo, gotFrom, gotBody)
	}
}

func TestTwilioSendErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"code":21211,"message":"Invalid 'To' Phone Number"}`))
	}))
	defer srv.Close()
	t.Setenv("GOPHISH_SMS_BASE_URL", srv.URL)

	sender, _ := New("twilio", "sid", "token")
	err := sender.Send(context.Background(), Message{To: "+1", From: "+1", Body: "x"})
	if err == nil {
		t.Fatal("expected an error for a 400 response")
	}
	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "Invalid 'To'") {
		t.Errorf("error = %v", err)
	}
}

func TestTwilioSendNoDestination(t *testing.T) {
	sender, _ := New("twilio", "sid", "token")
	if err := sender.Send(context.Background(), Message{From: "+1", Body: "x"}); err == nil {
		t.Fatal("expected an error when To is empty")
	}
}
