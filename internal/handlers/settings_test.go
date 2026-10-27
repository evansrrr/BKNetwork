package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bknetwork/internal/events"
	appsettings "bknetwork/internal/settings"
)

type sideEffectRecorder struct {
	calls     []string
	failOn    string
	failValue bool
}

func (r *sideEffectRecorder) startup(enabled bool) error {
	label := "startup=false"
	if enabled {
		label = "startup=true"
	}
	r.calls = append(r.calls, label)
	if r.failOn == "startup" && enabled == r.failValue {
		return errors.New("startup shortcut failed")
	}
	return nil
}

func (r *sideEffectRecorder) warpApp(enabled bool) error {
	label := "warpApp=false"
	if enabled {
		label = "warpApp=true"
	}
	r.calls = append(r.calls, label)
	if r.failOn == "warpApp" && enabled == r.failValue {
		return errors.New("warp shortcut failed")
	}
	return nil
}

func (r *sideEffectRecorder) effects() settingsSideEffects {
	return settingsSideEffects{
		applyStartupShortcut:        r.startup,
		applyWarpAppStartupShortcut: r.warpApp,
	}
}

func TestApplySettingsChangeNoop(t *testing.T) {
	recorder := &sideEffectRecorder{}
	var saved *appsettings.Settings
	save := func(cfg appsettings.Settings) error {
		saved = &cfg
		return nil
	}

	result, err := applySettingsChange(appsettings.Settings{}, appsettings.Settings{}, recorder.effects(), save)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recorder.calls) != 0 {
		t.Fatalf("side effects = %v, want none", recorder.calls)
	}
	if len(result.Applied) != 0 {
		t.Fatalf("applied = %v, want none", result.Applied)
	}
	if saved == nil {
		t.Fatal("settings were not saved")
	}
}

func TestApplySettingsChangeAppliesBeforeSaving(t *testing.T) {
	recorder := &sideEffectRecorder{}
	var order []string
	save := func(cfg appsettings.Settings) error {
		order = append(order, "save")
		return nil
	}

	_, err := applySettingsChange(
		appsettings.Settings{},
		appsettings.Settings{AutoStart: true, WarpAppAutoStart: true},
		recorder.effects(),
		save,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"startup=true", "warpApp=true", "save"}
	if strings.Join(recorder.calls, ",")+",save" != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", append(recorder.calls, order...), want)
	}
}

func TestApplySettingsChangeRollsBackWhenSecondEffectFails(t *testing.T) {
	recorder := &sideEffectRecorder{failOn: "warpApp", failValue: true}
	saveCalled := false
	save := func(cfg appsettings.Settings) error {
		saveCalled = true
		return nil
	}

	_, err := applySettingsChange(
		appsettings.Settings{},
		appsettings.Settings{AutoStart: true, WarpAppAutoStart: true},
		recorder.effects(),
		save,
	)
	if err == nil {
		t.Fatal("expected an error")
	}
	if saveCalled {
		t.Fatal("settings must not be saved when an effect failed")
	}
	want := []string{"startup=true", "warpApp=true", "startup=false"}
	if strings.Join(recorder.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v (last call reverts autostart)", recorder.calls, want)
	}
}

func TestApplySettingsChangeRollsBackWhenSaveFails(t *testing.T) {
	recorder := &sideEffectRecorder{}
	save := func(cfg appsettings.Settings) error {
		return errors.New("disk full")
	}

	_, err := applySettingsChange(
		appsettings.Settings{AutoStart: false, WarpAppAutoStart: false},
		appsettings.Settings{AutoStart: true, WarpAppAutoStart: true},
		recorder.effects(),
		save,
	)
	if err == nil {
		t.Fatal("expected an error")
	}
	want := []string{"startup=true", "warpApp=true", "warpApp=false", "startup=false"}
	if strings.Join(recorder.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v (reverse order rollback)", recorder.calls, want)
	}
}

func TestSettingsHandlerGetKeepsTopLevelFields(t *testing.T) {
	deps := settingsDeps{
		load: func() (appsettings.Settings, error) {
			return appsettings.Settings{AutoStart: true, SilentStart: true}, nil
		},
		save: func(appsettings.Settings) error { return nil },
	}
	handler := settingsHandler(events.NewHub(), deps, (&sideEffectRecorder{}).effects())

	response := httptest.NewRecorder()
	handler(response, httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	// The UI reads data.autoStart directly, so the shape must stay flat.
	if body["autoStart"] != true || body["silentStart"] != true {
		t.Fatalf("body = %v, want autoStart/silentStart at the top level", body)
	}
	if _, ok := body["warning"]; ok {
		t.Fatalf("body = %v, want no warning on a healthy read", body)
	}
}

func TestSettingsHandlerRecoversFromCorruptFile(t *testing.T) {
	loadErr := errors.New(`parse settings file: unexpected end of JSON input`)
	var saved appsettings.Settings
	deps := settingsDeps{
		load: func() (appsettings.Settings, error) { return appsettings.Settings{}, loadErr },
		save: func(cfg appsettings.Settings) error {
			saved = cfg
			return nil
		},
	}
	handler := settingsHandler(events.NewHub(), deps, (&sideEffectRecorder{}).effects())

	// GET reports the problem instead of failing the panel.
	getResponse := httptest.NewRecorder()
	handler(getResponse, httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil))
	if getResponse.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", getResponse.Code)
	}
	var getBody map[string]any
	if err := json.Unmarshal(getResponse.Body.Bytes(), &getBody); err != nil {
		t.Fatalf("decode GET body: %v", err)
	}
	if getBody["warning"] == nil {
		t.Fatalf("GET body = %v, want a warning", getBody)
	}

	// POST still saves, which repairs the damaged file.
	postBody := strings.NewReader(`{"autoStart":true,"silentStart":false,"warpAutoStart":false,"warpAppAutoStart":false}`)
	postRequest := httptest.NewRequest(http.MethodPost, "/api/v1/settings", postBody)
	postRequest.Header.Set("Content-Type", "application/json")
	postResponse := httptest.NewRecorder()
	handler(postResponse, postRequest)

	if postResponse.Code != http.StatusOK {
		t.Fatalf("POST status = %d, want 200: %s", postResponse.Code, postResponse.Body.String())
	}
	if !saved.AutoStart {
		t.Fatalf("saved = %+v, want autoStart true", saved)
	}
}

func TestSettingsHandlerReportsEffectFailure(t *testing.T) {
	saveCalled := false
	deps := settingsDeps{
		load: func() (appsettings.Settings, error) { return appsettings.Settings{}, nil },
		save: func(appsettings.Settings) error {
			saveCalled = true
			return nil
		},
	}
	recorder := &sideEffectRecorder{failOn: "startup", failValue: true}
	handler := settingsHandler(events.NewHub(), deps, recorder.effects())

	body := strings.NewReader(`{"autoStart":true}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/settings", body)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
	if saveCalled {
		t.Fatal("settings must not be saved when applying the side effect fails")
	}
	if !strings.Contains(response.Body.String(), "apply autostart") {
		t.Fatalf("body = %s, want the failing step in the detail", response.Body.String())
	}
}
