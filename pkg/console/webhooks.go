package console

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"

	"github.com/pkg/errors"
	"github.com/rancher/harvester-installer/pkg/config"
	"github.com/rancher/harvester-installer/pkg/util"
	"github.com/sirupsen/logrus"
)

type RenderedWebhook struct {
	config.Webhook
	RenderedURL     string
	RenderedPayload string
}

type RendererWebhooks []RenderedWebhook

const (
	EventInstallStarted   = "STARTED"
	EventInstallCompleted = "COMPLETED"
)

func IsValidEvent(event string) bool {
	events := []string{
		EventInstallStarted,
		EventInstallCompleted,
	}
	return util.StringSliceContains(events, event)
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
	return util.StringSliceContains(methods, method)
}

func (p *RenderedWebhook) Handle() error {
	logrus.Debugf("Handle webhook: %+v", p)
	c := http.Client{
		Timeout: defaultHTTPTimeout,
	}

	if p.Webhook.Insecure {
		c.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}
	}

	var body io.Reader
	if p.RenderedPayload != "" {
		body = strings.NewReader(p.RenderedPayload)
	}

	req, err := http.NewRequest(p.Webhook.Method, p.RenderedURL, body)
	if err != nil {
		return err
	}

	if p.BasicAuth.User != "" && p.BasicAuth.Password != "" {
		req.SetBasicAuth(p.BasicAuth.User, p.BasicAuth.Password)
	}

	for k, vv := range p.Webhook.Headers {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}

	resp, err := c.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("got %d status code from %s", resp.StatusCode, p.RenderedURL)
	}
	return nil
}

func dupHeaders(h map[string][]string) map[string][]string {
	if h == nil {
		return nil
	}
	m := make(map[string][]string)
	for k, v := range h {
		m[k] = util.DupStrings(v)
	}
	return m
}

func prepareWebhook(h config.Webhook, context map[string]string) (*RenderedWebhook, error) {
	logrus.Debugf("Preparing webhook %+v", h)

	p := &RenderedWebhook{
		Webhook: config.Webhook{
			Event:    h.Event,
			Method:   strings.ToUpper(h.Method),
			Headers:  dupHeaders(h.Headers),
			URL:      h.URL,
			Payload:  h.Payload,
			Insecure: h.Insecure,
			BasicAuth: config.HTTPBasicAuth{
				User:     h.BasicAuth.User,
				Password: h.BasicAuth.Password,
			},
		},
	}

	if !IsValidEvent(p.Webhook.Event) {
		return nil, errors.Errorf("unknown install event: %s", p.Webhook.Event)
	}
	if !IsValidHttpMethod(p.Webhook.Method) {
		return nil, errors.Errorf("unknown HTTP method: %s", p.Webhook.Method)
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

func PrepareWebhooks(hooks []config.Webhook, context map[string]string) (RendererWebhooks, error) {
	var result RendererWebhooks
	for _, h := range hooks {
		p, err := prepareWebhook(h, context)
		if err != nil {
			return nil, err
		}
		result = append(result, *p)
	}
	return result, nil
}

func (hooks RendererWebhooks) Handle(event string) error {
	logrus.Infof("Handle webhooks for event %s", event)
	for _, h := range hooks {
		if event != h.Webhook.Event {
			continue
		}
		if err := h.Handle(); err != nil {
			logrus.Errorf("fail to handle webhook: %s", err)
			return err
		}
	}
	return nil
}
