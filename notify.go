package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Notifications go to channels the operator configures: e-mail (own SMTP), a generic webhook
// (payload works for Slack, Discord, Mattermost and plain JSON receivers) or an ntfy topic.

// Events a channel can subscribe to.
var notifyEvents = []string{
	"new_device",       // successful sign-in from a device/IP not seen for this user before
	"ip_locked",        // brute-force protection locked an IP address
	"admin_signin",     // an admin signed in
	"settings_changed", // sites, users or settings were changed
}

type NotifyChannel struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`   // email | webhook | ntfy
	Target  string   `json:"target"` // e-mail address, webhook URL or ntfy topic URL
	Token   string   `json:"token,omitempty"`
	Events  []string `json:"events"`
	Enabled bool     `json:"enabled"`
}

type Notification struct {
	Event   string
	Title   string
	Message string
	Link    string
}

func validNotifyChannel(c *NotifyChannel) error {
	c.Name = strings.TrimSpace(c.Name)
	c.Target = strings.TrimSpace(c.Target)
	switch c.Type {
	case "email":
		if !strings.Contains(c.Target, "@") {
			return fmt.Errorf("invalid e-mail address")
		}
	case "webhook", "ntfy":
		u, err := url.Parse(c.Target)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return fmt.Errorf("invalid URL")
		}
	default:
		return fmt.Errorf("unknown channel type %q", c.Type)
	}
	known := map[string]bool{}
	for _, e := range notifyEvents {
		known[e] = true
	}
	events := []string{}
	for _, e := range c.Events {
		if known[e] {
			events = append(events, e)
		}
	}
	c.Events = events
	return nil
}

func (c NotifyChannel) wants(event string) bool {
	if !c.Enabled {
		return false
	}
	for _, e := range c.Events {
		if e == event {
			return true
		}
	}
	return false
}

var notifyClient = &http.Client{Timeout: 10 * time.Second}

// deliver sends one notification to one channel.
func deliver(c NotifyChannel, smtpCfg SMTPConfig, n Notification) error {
	text := n.Message
	if n.Link != "" {
		text += "\n\n" + n.Link
	}
	switch c.Type {
	case "email":
		return sendMail(smtpCfg, Mail{To: c.Target, Subject: "[Wicket] " + n.Title, Text: text})
	case "webhook":
		payload := map[string]any{
			"event": n.Event, "title": n.Title, "message": n.Message, "link": n.Link,
			"time":    time.Now().UTC().Format(time.RFC3339),
			"text":    "*" + n.Title + "*\n" + text,   // Slack, Mattermost
			"content": "**" + n.Title + "**\n" + text, // Discord
		}
		body, _ := json.Marshal(payload)
		req, err := http.NewRequest(http.MethodPost, c.Target, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
		return doNotify(req)
	case "ntfy":
		req, err := http.NewRequest(http.MethodPost, c.Target, strings.NewReader(text))
		if err != nil {
			return err
		}
		req.Header.Set("Title", n.Title)
		req.Header.Set("Tags", "lock")
		if n.Link != "" {
			req.Header.Set("Click", n.Link)
		}
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
		return doNotify(req)
	}
	return fmt.Errorf("unknown channel type %q", c.Type)
}

func doNotify(req *http.Request) error {
	resp, err := notifyClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s answered %s", req.URL.Host, resp.Status)
	}
	return nil
}

// notifyAll delivers n to every subscribed channel in the background; failures are only logged.
func notifyAll(channels []NotifyChannel, smtpCfg SMTPConfig, n Notification) {
	for _, c := range channels {
		if !c.wants(n.Event) {
			continue
		}
		go func(c NotifyChannel) {
			if err := deliver(c, smtpCfg, n); err != nil {
				log.Printf("notify %s (%s): %v", c.Name, c.Type, err)
			}
		}(c)
	}
}
