package gate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGroupModeDeliversWithoutPersonalIdentity(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	c := &Controller{Config: DefaultConfig(), Store: store, Notifier: &fakeNotify{}}
	json.Unmarshal([]byte(`{"notification_mode":"feishu_group"}`), &c.Config)
	r := &Run{Key: "run", Scope: c.scope(), PR: PR{Number: 1, User: User{ID: 37}}, ReportID: 1, Status: "success"}
	store.Runs["run"] = r
	if err := c.drainOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !r.Notified || r.NotifyError != "" || c.Notifier.(*fakeNotify).calls != 1 {
		t.Fatal("group result not delivered")
	}
	if err := c.drainOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.Notifier.(*fakeNotify).calls != 1 {
		t.Fatal("duplicate accepted group result")
	}
}

func TestNotificationModeConfig(t *testing.T) {
	for _, mode := range []string{"feishu_dm", "feishu_group", "invalid"} {
		c := DefaultConfig()
		c.AllowInsecureHTTP = true
		c.BotUsername = "review-bot"
		raw, _ := json.Marshal(c)
		var obj map[string]any
		json.Unmarshal(raw, &obj)
		obj["notification_mode"] = mode
		raw, _ = json.Marshal(obj)
		p := filepath.Join(t.TempDir(), "config.json")
		os.WriteFile(p, raw, 0600)
		_, err := LoadConfig(p)
		if mode == "invalid" && err == nil {
			t.Fatal("accepted unknown mode")
		}
		if mode != "invalid" && err != nil {
			t.Fatal(err)
		}
	}
}

func TestFailedGroupCanSwitchToDM(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	n := &fakeNotify{fail: true}
	c := &Controller{Config: DefaultConfig(), Store: store, Notifier: n}
	c.Config.NotificationMode = "feishu_group"
	r := &Run{Key: "run", Scope: c.scope(), PR: PR{Number: 1, User: User{ID: 37}}, ReportID: 1, Status: "success"}
	store.Runs[r.Key] = r
	if c.drainOutbox(context.Background()) == nil || r.Notified {
		t.Fatal("failed group was acknowledged")
	}
	if r.Recipient != "" {
		t.Fatal("group overwrote DM identity")
	}
	c.Config.NotificationMode = "feishu_dm"
	c.Config.Identities = map[string]string{"37": "ou_verified"}
	n.fail = false
	if err := c.drainOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !r.Notified || r.Recipient != "ou_verified" || n.calls != 2 {
		t.Fatal("DM retry did not resolve identity")
	}
}
