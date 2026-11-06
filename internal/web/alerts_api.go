package web

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ocochard/cmonit/internal/alert"
)

var alertEngine *alert.Engine

// HandleAlertsPage serves the alert configuration page. It is static HTML; the
// page's JavaScript talks to /api/alerts/.
func HandleAlertsPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/alerts" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, "alerts.html", struct{ AppVersion string }{appVersion}); err != nil {
		log.Printf("[ERROR] Failed to render alerts template: %v", err)
	}
}

// SetAlertEngine sets the engine used for channel test sends.
func SetAlertEngine(e *alert.Engine) {
	alertEngine = e
}

// HandleAlertsAPI serves /api/alerts/...:
//
//	GET|POST        /api/alerts/channels
//	PUT|DELETE      /api/alerts/channels/{id}
//	POST            /api/alerts/channels/{id}/test
//	GET|POST        /api/alerts/rules
//	PUT|DELETE      /api/alerts/rules/{id}
//	GET|POST        /api/alerts/silences
//	DELETE          /api/alerts/silences/{id}
//	GET             /api/alerts/log?limit=N
//
// Channel secrets are masked in responses; sending the masked value back on
// PUT keeps the stored secret.
func HandleAlertsAPI(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/alerts/"), "/"), "/")

	// A cross-site form cannot send application/json without a CORS preflight,
	// which blocks CSRF against the endpoints that take a body.
	hasBody := r.Method == http.MethodPost || r.Method == http.MethodPut
	isTest := len(parts) == 3 && parts[2] == "test"
	if hasBody && !isTest && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		respondJSON(w, map[string]string{"error": "Content-Type must be application/json"}, http.StatusUnsupportedMediaType)
		return
	}

	var id int64
	if len(parts) >= 2 {
		var err error
		if id, err = strconv.ParseInt(parts[1], 10, 64); err != nil {
			respondJSON(w, map[string]string{"error": "invalid id"}, http.StatusBadRequest)
			return
		}
	}

	switch {
	case parts[0] == "channels" && len(parts) == 1:
		alertChannelsCollection(w, r)
	case parts[0] == "channels" && len(parts) == 2:
		alertChannelItem(w, r, id)
	case parts[0] == "channels" && len(parts) == 3 && parts[2] == "test" && r.Method == http.MethodPost:
		alertChannelTest(w, r, id)
	case parts[0] == "rules" && len(parts) == 1:
		alertRulesCollection(w, r)
	case parts[0] == "rules" && len(parts) == 2:
		alertRuleItem(w, r, id)
	case parts[0] == "silences" && len(parts) == 1:
		alertSilencesCollection(w, r)
	case parts[0] == "silences" && len(parts) == 2 && r.Method == http.MethodDelete:
		if err := alert.DeleteSilence(db, id); err != nil {
			alertError(w, err)
			return
		}
		respondJSON(w, map[string]string{"status": "deleted"}, http.StatusOK)
	case parts[0] == "log" && len(parts) == 1 && r.Method == http.MethodGet:
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		entries, err := alert.ListLog(db, limit)
		if err != nil {
			alertError(w, err)
			return
		}
		respondJSON(w, entries, http.StatusOK)
	default:
		respondJSON(w, map[string]string{"error": "not found"}, http.StatusNotFound)
	}
}

func alertError(w http.ResponseWriter, err error) {
	msg := err.Error()
	switch {
	case errors.Is(err, alert.ErrNotFound):
		respondJSON(w, map[string]string{"error": "not found"}, http.StatusNotFound)
	case alert.IsInvalid(err):
		respondJSON(w, map[string]string{"error": msg}, http.StatusBadRequest)
	case strings.Contains(msg, "UNIQUE"):
		respondJSON(w, map[string]string{"error": "name already exists"}, http.StatusConflict)
	case strings.Contains(msg, "FOREIGN KEY"):
		respondJSON(w, map[string]string{"error": "unknown channel id"}, http.StatusBadRequest)
	default:
		log.Printf("[ERROR] Alerts API: %v", err)
		respondJSON(w, map[string]string{"error": "internal error"}, http.StatusInternalServerError)
	}
}

func maskChannel(c alert.Channel) alert.Channel {
	c.Config = alert.MaskConfig(c.Config)
	return c
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		respondJSON(w, map[string]string{"error": "invalid JSON body"}, http.StatusBadRequest)
		return false
	}
	return true
}

func alertChannelsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := alert.ListChannels(db)
		if err != nil {
			alertError(w, err)
			return
		}
		for i := range list {
			list[i] = maskChannel(list[i])
		}
		respondJSON(w, list, http.StatusOK)
	case http.MethodPost:
		var c alert.Channel
		if !decodeBody(w, r, &c) {
			return
		}
		c.ID = 0
		if err := alert.SaveChannel(db, &c); err != nil {
			alertError(w, err)
			return
		}
		respondJSON(w, maskChannel(c), http.StatusCreated)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func alertChannelItem(w http.ResponseWriter, r *http.Request, id int64) {
	switch r.Method {
	case http.MethodPut:
		var c alert.Channel
		if !decodeBody(w, r, &c) {
			return
		}
		c.ID = id
		if err := alert.SaveChannel(db, &c); err != nil {
			alertError(w, err)
			return
		}
		stored, err := alert.GetChannel(db, id)
		if err != nil {
			alertError(w, err)
			return
		}
		respondJSON(w, maskChannel(stored), http.StatusOK)
	case http.MethodDelete:
		if err := alert.DeleteChannel(db, id); err != nil {
			alertError(w, err)
			return
		}
		respondJSON(w, map[string]string{"status": "deleted"}, http.StatusOK)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func alertChannelTest(w http.ResponseWriter, r *http.Request, id int64) {
	if alertEngine == nil {
		respondJSON(w, map[string]string{"error": "alerting not initialized"}, http.StatusServiceUnavailable)
		return
	}
	c, err := alert.GetChannel(db, id)
	if err != nil {
		alertError(w, err)
		return
	}
	if err := alertEngine.SendTest(r.Context(), c); err != nil {
		respondJSON(w, map[string]string{"status": "failed", "error": err.Error()}, http.StatusBadGateway)
		return
	}
	respondJSON(w, map[string]string{"status": "sent"}, http.StatusOK)
}

func alertRulesCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := alert.ListRules(db)
		if err != nil {
			alertError(w, err)
			return
		}
		respondJSON(w, list, http.StatusOK)
	case http.MethodPost:
		rule := alert.Rule{Enabled: true, NotifyRecovery: true, HoldSeconds: 30}
		if !decodeBody(w, r, &rule) {
			return
		}
		rule.ID = 0
		if err := alert.SaveRule(db, &rule); err != nil {
			alertError(w, err)
			return
		}
		saved, err := alert.GetRule(db, rule.ID)
		if err != nil {
			alertError(w, err)
			return
		}
		respondJSON(w, saved, http.StatusCreated)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func alertRuleItem(w http.ResponseWriter, r *http.Request, id int64) {
	switch r.Method {
	case http.MethodPut:
		rule, err := alert.GetRule(db, id)
		if err != nil {
			alertError(w, err)
			return
		}
		// Decode onto the stored rule so omitted fields keep their value.
		if !decodeBody(w, r, &rule) {
			return
		}
		rule.ID = id
		if err := alert.SaveRule(db, &rule); err != nil {
			alertError(w, err)
			return
		}
		saved, err := alert.GetRule(db, id)
		if err != nil {
			alertError(w, err)
			return
		}
		respondJSON(w, saved, http.StatusOK)
	case http.MethodDelete:
		if err := alert.DeleteRule(db, id); err != nil {
			alertError(w, err)
			return
		}
		respondJSON(w, map[string]string{"status": "deleted"}, http.StatusOK)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func alertSilencesCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := alert.ListActiveSilences(db, time.Now())
		if err != nil {
			alertError(w, err)
			return
		}
		respondJSON(w, list, http.StatusOK)
	case http.MethodPost:
		var sl alert.Silence
		if !decodeBody(w, r, &sl) {
			return
		}
		sl.ID = 0
		if err := alert.SaveSilence(db, &sl, time.Now()); err != nil {
			alertError(w, err)
			return
		}
		sl.DurationSeconds = 0
		respondJSON(w, sl, http.StatusCreated)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
