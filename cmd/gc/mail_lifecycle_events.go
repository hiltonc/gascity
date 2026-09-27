package main

import (
	"github.com/gastownhall/gascity/internal/events"
)

// mailSystemActor is the event actor for mail the controller changes on its
// own schedule (the retention sweep and the read-mail purge), as opposed to a
// user or agent acting through the CLI or API.
const mailSystemActor = "controller"

// recordMailLifecycleEvents announces each message a sweep or purge changed.
// Every event advances the city event index, so a client syncing mail by
// "what changed since N" learns that a message was archived or deleted even
// though no user asked for it. A nil recorder records nothing.
func recordMailLifecycleEvents(rec events.Recorder, eventType, actor string, ids []string) {
	if rec == nil {
		return
	}
	for _, id := range ids {
		rec.Record(events.Event{
			Type:    eventType,
			Actor:   actor,
			Subject: id,
			Payload: mailEventPayload(nil),
		})
	}
}
