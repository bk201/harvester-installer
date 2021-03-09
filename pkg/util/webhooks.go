package util

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/pkg/errors"
	"github.com/rancher/harvester-installer/pkg/config"
	"github.com/sirupsen/logrus"
)

type ParsedWebhook struct {
	config.Webhook
	RenderedURL     string
	RenderedPayload string
}

type ParsedWebhooks []ParsedWebhook

const (
	EventInstallStarted   = "STARTED"
	EventInstallCompleted = "COMPLETED"
)

func IsValidEvent(event string) bool {
	events := []string{
		EventInstallStarted,
		EventInstallCompleted,
	}
	return StringSliceContains(events, event)
}

func IsValidHttpMethod(method string) bool {
	methods := []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodConnect,
		http.MethodOptions,
		http.MethodTrace,
	}
	return StringSliceContains(methods, method)
}

func parseMethod(method string) (string, error) {
	if IsValidHttpMethod(method) {
		return method, nil
	}
	return "", errors.Errorf("unknonw http method: %s", method)
}

func (p *ParsedWebhook) Send() error {
	c := http.Client{
		Timeout: 15 * time.Second,
	}
	logrus.Debugf("%s %s body: %s", p.Webhook.Method, p.RenderedURL, p.RenderedPayload)

	var body io.Reader
	if p.RenderedPayload != "" {
		body = strings.NewReader(p.RenderedPayload)
	}

	req, err := http.NewRequest(p.Webhook.Method, p.RenderedURL, body)
	if err != nil {
		return err
	}

	_, err = c.Do(req)
	if err != nil {
		return err
	}
	return nil
}

func dupHeaders(h map[string][]string) map[string][]string {
	if h == nil {
		return nil
	}
	m := make(map[string][]string)
	for k, v := range h {
		m[k] = DupStrings(v)
	}
	return m
}

func parseWebhook(h config.Webhook, context map[string]string) (*ParsedWebhook, error) {
	logrus.Debugf("Parsing webhook %+v", h)
	// p := &ParsedWebhook{
	// 	config.Webhook{
	// 		Event:   h.Event,
	// 		Method:  strings.ToUpper(h.Method),
	// 		URL:     h.URL,
	// 		Payload: h.Payload,
	// 	},
	// 	"",
	// 	"",
	// }

	p := &ParsedWebhook{
		Webhook: config.Webhook{
			Event:   h.Event,
			Method:  strings.ToUpper(h.Method),
			Headers: dupHeaders(h.Headers),
			URL:     h.URL,
			Payload: h.Payload,
		},
	}

	if !IsValidEvent(p.Webhook.Event) {
		return nil, errors.Errorf("unknown install event: %q", p.Webhook.Event)
	}
	if !IsValidHttpMethod(p.Webhook.Method) {
		return nil, errors.Errorf("unknown HTTP method: %q", p.Webhook.Method)
	}

	// render URL
	bs := bytes.NewBufferString("")
	tmpl, err := template.New("URL").Parse(p.Webhook.URL)
	if err != nil {
		return nil, err
	}
	err = tmpl.Execute(bs, context)
	if err != nil {
		return nil, err
	}
	p.RenderedURL = bs.String()

	// render payload
	bs.Reset()
	tmpl, err = template.New("Payload").Parse(p.Webhook.Payload)
	if err != nil {
		return nil, err
	}
	err = tmpl.Execute(bs, context)
	if err != nil {
		return nil, err
	}
	p.RenderedPayload = bs.String()

	return p, nil
}

func ParseWebhooks(hooks []config.Webhook, context map[string]string) (ParsedWebhooks, error) {
	var result ParsedWebhooks
	for _, h := range hooks {
		parsed, err := parseWebhook(h, context)
		if err != nil {
			return nil, err
		}
		result = append(result, *parsed)
	}
	return result, nil
}

func (hooks ParsedWebhooks) Send(event string) error {
	logrus.Infof("Handle webhooks for event %q", event)
	for _, h := range hooks {
		if event != h.Webhook.Event {
			continue
		}
		if err := h.Send(); err != nil {
			logrus.Errorf("fail to execute webhook: %s", err)
			return err
		}
	}
	return nil
}
