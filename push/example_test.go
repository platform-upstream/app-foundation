package push_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"github.com/platform-upstream/app-foundation/push"
	"github.com/platform-upstream/app-foundation/sqlite"
)

// An app tells the owner with Notify. On a platform with a hub its chart sets the hub's URL and
// the app's key there, and the message goes to the hub; without them, or when the hub fails, the
// payload the app's service worker expects goes to the app's own subscriptions, as with Send.
func ExampleSender_Notify() {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg map[string]string
		_ = json.NewDecoder(r.Body).Decode(&msg)
		fmt.Println("hub:", msg["title"], "|", msg["ref"], "|", msg["urgency"])
		fmt.Fprint(w, `{"id":"invoice-42"}`)
	}))
	defer hub.Close()

	ctx := context.Background()
	dir, _ := os.MkdirTemp("", "push-example")
	defer os.RemoveAll(dir)
	// The app's database; its own migration creates push_subscriptions.
	db, err := sqlite.Open(ctx, filepath.Join(dir, "acme.sqlite"), sqlite.Options{})
	if err != nil {
		panic(err)
	}
	defer db.Close()

	sender := push.New(db, push.Config{
		PublicKey: os.Getenv("WEB_PUSH_VAPID_PUBLIC"), PrivateKey: os.Getenv("WEB_PUSH_VAPID_PRIVATE"), Subject: os.Getenv("WEB_PUSH_SUBJECT"),
		// In an app: os.Getenv("PUSH_HUB_URL") and os.Getenv("PUSH_HUB_KEY"); both unset, no hub.
		Hub: push.HubConfig{URL: hub.URL + "/internal/notify", Key: strings.Repeat("k", 32)},
	})

	payload, _ := json.Marshal(map[string]string{"title": "Invoice due", "url": "/invoices/42"}) // the app's own shape
	delivery, err := sender.Notify(ctx, push.Message{
		Title: "Invoice due", Body: "Acme Ltd, 120.00, due Friday.", Ref: "acme:invoice/42",
		Urgency: push.UrgencyLater, ID: "invoice-42",
	}, payload)
	fmt.Println(delivery.Hub, delivery.Devices, err)
	// Output:
	// hub: Invoice due | acme:invoice/42 | later
	// true 0 <nil>
}
