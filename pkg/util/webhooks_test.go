package util

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rancher/harvester-installer/pkg/config"
	"github.com/stretchr/testify/assert"
)

func TestParseWebhook(t *testing.T) {
	tests := []struct {
		name          string
		unparsed      config.Webhook
		context       map[string]string
		parsedURL     string
		parsedPayload string
		errorString   string
	}{
		{
			name: "valid",
			unparsed: config.Webhook{
				Event:  "COMPLETED",
				Method: "get",
				Headers: map[string][]string{
					"Content-Type": {"application/json", "charset=utf-8"},
				},
				URL:     "http://10.100.0.10/cblr/svc/op/nopxe/system/{{.Hostname}}",
				Payload: `{"hostname": "{{.Hostname}}"}`,
			},
			context: map[string]string{
				"Hostname": "node1",
			},
			parsedURL:     "http://10.100.0.10/cblr/svc/op/nopxe/system/node1",
			parsedPayload: `{"hostname": "node1"}`,
		},
		{
			name: "invalid event",
			unparsed: config.Webhook{
				Event:  "XXX",
				Method: "GET",
				URL:    "http://somewhere.com",
			},
			errorString: "unknown install event: \"XXX\"",
		},
		{
			name: "invalid HTTP method",
			unparsed: config.Webhook{
				Event:  "STARTED",
				Method: "PUNCH",
				URL:    "http://somewhere.com",
			},
			errorString: "unknown HTTP method: \"PUNCH\"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := parseWebhook(tt.unparsed, tt.context)
			if tt.errorString != "" {
				assert.EqualError(t, err, tt.errorString)
			} else {
				assert.Equal(t, tt.parsedURL, p.RenderedURL)
				assert.Equal(t, tt.parsedPayload, p.RenderedPayload)
				assert.Equal(t, nil, err)
			}
		})
	}
}

func TestParsedWebhook_Send(t *testing.T) {
	type fields struct {
		Webhook         config.Webhook
		RenderedURL     string
		RenderedPayload string
	}
	type RequestRecorder struct {
		Method  string
		Body    string
		Handled bool
	}

	tests := []struct {
		name       string
		fields     fields
		wantMethod string
		wantBody   string
	}{
		{
			name: "get a url",
			fields: fields{
				Webhook: config.Webhook{Method: "GET"},
			},
			wantMethod: "GET",
		},
		{
			name: "put a body",
			fields: fields{
				Webhook:         config.Webhook{Method: "PUT"},
				RenderedPayload: "data",
			},
			wantMethod: "PUT",
			wantBody:   "data",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := RequestRecorder{}
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				recorder.Method = r.Method
				defer r.Body.Close()
				body, err := ioutil.ReadAll(r.Body)
				if err != nil {
					return
				}
				recorder.Body = string(body)
				recorder.Handled = true
			}))
			defer ts.Close()

			p := &ParsedWebhook{
				Webhook:         tt.fields.Webhook,
				RenderedURL:     ts.URL,
				RenderedPayload: tt.fields.RenderedPayload,
			}
			p.Send()
			assert.Equal(t, tt.wantMethod, recorder.Method)
			assert.Equal(t, tt.wantBody, recorder.Body)
			assert.Equal(t, true, recorder.Handled)
		})
	}
}
