package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// A one-pixel PNG, which is a real picture and sniffs as one.
var pngPixel = []byte{
	0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 'I', 'D', 'A', 'T',
	0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05,
	0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00,
	0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
}

// upload is one file in a multipart send.
type upload struct {
	field   string
	name    string
	content []byte
}

// postFile sends a multipart form the way the composer does.
func (a *app) postFile(path, field, name string, content []byte, fields url.Values) (int, string) {
	a.t.Helper()
	var files []upload
	if name != "" {
		files = []upload{{field: field, name: name, content: content}}
	}
	return a.postFiles(path, files, fields)
}

// postFiles is the composer sending several at once.
func (a *app) postFiles(path string, files []upload, fields url.Values) (int, string) {
	a.t.Helper()

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("csrf_token", a.csrf())
	for key, values := range fields {
		for _, v := range values {
			_ = form.WriteField(key, v)
		}
	}
	for _, f := range files {
		part, err := form.CreateFormFile(f.field, f.name)
		if err != nil {
			a.t.Fatalf("form file: %v", err)
		}
		if _, err := part.Write(f.content); err != nil {
			a.t.Fatalf("write file: %v", err)
		}
	}
	form.Close()

	req, err := http.NewRequest(http.MethodPost, a.server.URL+path, &body)
	if err != nil {
		a.t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	res, err := a.client.Do(req)
	if err != nil {
		a.t.Fatalf("POST %s: %v", path, err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(out)
}

// A message may be a photograph with nothing said about it, and the file it
// carries reaches the other person and nobody else.
func TestAPictureIsAMessageAndOnlyTheTwoOfThemSeeIt(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	conv := pair(t, a, b, "shotone", "shottwo")

	if code, _ := a.postFile("/messages/"+conv, "file", "dome.png", pngPixel, nil); code != http.StatusSeeOther {
		t.Fatalf("sending a picture with no words → %d", code)
	}

	code, body := b.get("/messages/" + conv + "/poll?after=0")
	if code != http.StatusOK {
		t.Fatalf("poll → %d", code)
	}
	var page struct {
		Messages []struct {
			Body string `json:"body"`
			File *struct {
				ID   string `json:"id"`
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"file"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("poll json: %v", err)
	}
	if len(page.Messages) != 1 || page.Messages[0].File == nil {
		t.Fatalf("the picture did not arrive as a message: %s", body)
	}
	file := page.Messages[0].File
	if file.Kind != "image" {
		t.Errorf("a PNG arrived as %q", file.Kind)
	}
	if file.Name != "dome.png" {
		t.Errorf("the name came through as %q", file.Name)
	}

	// The two of them can read it back.
	if code, _ := a.get("/files/" + file.ID); code != http.StatusOK {
		t.Errorf("the sender cannot fetch their own file → %d", code)
	}
	if code, _ := b.get("/files/" + file.ID); code != http.StatusOK {
		t.Errorf("the recipient cannot fetch the file → %d", code)
	}

	// Nobody else can, even holding the id.
	stranger := newAppSharing(t, a)
	stranger.register("shotnosy")
	if code, _ := stranger.get("/files/" + file.ID); code != http.StatusNotFound {
		t.Errorf("somebody outside the conversation fetched the file → %d, want 404", code)
	}
}

// Content-Type is whatever the client felt like saying. What the file actually
// is decides whether it is accepted.
func TestAnUploadIsJudgedByItsBytesNotItsName(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	conv := pair(t, a, b, "sniffone", "snifftwo")

	script := []byte("<script>alert(1)</script>")
	a.postFile("/messages/"+conv, "file", "picture.png", script,
		url.Values{"body": {"look at this"}})

	// Refused, so neither the file nor the message it rode in on exists.
	_, thread := b.get("/messages/" + conv)
	if strings.Contains(thread, "picture.png") {
		t.Error("a script named picture.png was accepted as an upload")
	}
	if strings.Contains(thread, "look at this") {
		t.Error("the refused upload still produced a message")
	}

	// A real picture under the same name goes through, so the refusal was
	// about the bytes and not about the name.
	a.postFile("/messages/"+conv, "file", "picture.png", pngPixel, nil)
	if _, thread := b.get("/messages/" + conv); !strings.Contains(thread, "picture.png") {
		t.Error("a real PNG was refused")
	}
}

// A profile photo has to be a picture, and once set it is what every avatar
// draws.
func TestAProfilePhotoReplacesTheGeneratedAvatar(t *testing.T) {
	a := newApp(t)
	a.register("photoperson")

	if code, _ := a.postFile("/profile/photo", "photo", "me.png", pngPixel, nil); code != http.StatusSeeOther {
		t.Fatalf("uploading a photo → %d", code)
	}

	_, profile := a.get("/u/photoperson")
	if !strings.Contains(profile, "avatar--photo") {
		t.Error("the profile still draws the generated gradient after a photo was saved")
	}

	user, err := a.repo.UserByUsername(t.Context(), "photoperson")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !strings.HasPrefix(user.AvatarSeed, "photo:") {
		t.Errorf("avatar_seed = %q, want a photo reference", user.AvatarSeed)
	}

	// And back again.
	if code, _ := a.post("/profile/photo/remove", url.Values{}); code != http.StatusSeeOther {
		t.Fatal("removing the photo was refused")
	}
	if user, _ := a.repo.UserByUsername(t.Context(), "photoperson"); user.AvatarSeed != "" {
		t.Errorf("avatar_seed = %q after removal, want empty", user.AvatarSeed)
	}
}

// The EBML header every WebM stream opens with. Go's sniffer answers
// "video/webm" for it, which is what MediaRecorder produces for a voice note
// and what the accepted list maps to audio.
var webmHeader = []byte{0x1a, 0x45, 0xdf, 0xa3, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x23}

// Picking four photographs is four things sent, not one bubble holding four
// that the reader cannot answer one of. The words typed above them ride with
// the first, because that is the one they were typed about.
func TestSeveralFilesArriveAsSeveralMessages(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	conv := pair(t, a, b, "manysender", "manyreader")

	code, _ := a.postFiles("/messages/"+conv, []upload{
		{field: "file", name: "one.png", content: pngPixel},
		{field: "file", name: "two.png", content: pngPixel},
		{field: "file", name: "three.png", content: pngPixel},
	}, url.Values{"body": {"three of them"}})
	if code != http.StatusSeeOther {
		t.Fatalf("sending three files → %d", code)
	}

	code, body := b.get("/messages/" + conv + "/poll?after=0")
	if code != http.StatusOK {
		t.Fatalf("poll → %d", code)
	}
	var said struct {
		Messages []struct {
			Body string `json:"body"`
			File *struct {
				Name string `json:"name"`
			} `json:"file"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &said); err != nil {
		t.Fatalf("poll answered %q", body)
	}
	if len(said.Messages) != 3 {
		t.Fatalf("three files arrived as %d messages", len(said.Messages))
	}
	for i, m := range said.Messages {
		if m.File == nil {
			t.Errorf("message %d carries no file", i)
		}
		if i == 0 && m.Body != "three of them" {
			t.Errorf("the words landed on message %d as %q", i, m.Body)
		}
		if i > 0 && m.Body != "" {
			t.Errorf("the words were repeated on message %d", i)
		}
	}
}

// One too many is refused before anything is stored. Sending six and having
// five arrive is worse than being told five is the limit.
func TestMoreFilesThanAllowedSendNothing(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	conv := pair(t, a, b, "toomanysender", "toomanyreader")

	files := make([]upload, 0, 6)
	for i := 0; i < 6; i++ {
		files = append(files, upload{field: "file", name: "shot.png", content: pngPixel})
	}
	if code, _ := a.postFiles("/messages/"+conv, files, nil); code != http.StatusSeeOther {
		t.Fatalf("sending six files → %d", code)
	}

	code, body := b.get("/messages/" + conv + "/poll?after=0")
	if code != http.StatusOK {
		t.Fatalf("poll → %d", code)
	}
	var said struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &said); err != nil {
		t.Fatalf("poll answered %q", body)
	}
	if len(said.Messages) != 0 {
		t.Errorf("a refused send still put %d messages in the thread", len(said.Messages))
	}
}

// A WebM stream from MediaRecorder carries no duration, so the only party that
// ever knows how long a voice note ran is the browser that recorded it. What
// it measured has to survive the send, or every recording plays as 0:00.
func TestAVoiceNoteKeepsTheLengthTheBrowserMeasured(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	conv := pair(t, a, b, "voicesender", "voicereader")

	code, _ := a.postFiles("/messages/"+conv,
		[]upload{{field: "file", name: "voice note.webm", content: webmHeader}},
		url.Values{"duration_ms": {"7400"}})
	if code != http.StatusSeeOther {
		t.Fatalf("sending a recording → %d", code)
	}

	code, body := b.get("/messages/" + conv + "/poll?after=0")
	if code != http.StatusOK {
		t.Fatalf("poll → %d", code)
	}
	var said struct {
		Messages []struct {
			File *struct {
				Kind   string `json:"kind"`
				Length string `json:"length"`
			} `json:"file"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &said); err != nil {
		t.Fatalf("poll answered %q", body)
	}
	if len(said.Messages) != 1 || said.Messages[0].File == nil {
		t.Fatalf("the recording did not arrive: %s", body)
	}
	if got := said.Messages[0].File.Kind; got != "audio" {
		t.Errorf("the recording arrived as %q", got)
	}
	if got := said.Messages[0].File.Length; got != "0:07" {
		t.Errorf("a 7.4 second recording reads as %q, want 0:07", got)
	}
}

// A length is a thing only a recording can have. A client claiming one for a
// photograph is describing something the file cannot be.
func TestOnlyARecordingKeepsALength(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	conv := pair(t, a, b, "lensender", "lenreader")

	a.postFiles("/messages/"+conv,
		[]upload{{field: "file", name: "dome.png", content: pngPixel}},
		url.Values{"duration_ms": {"9000"}})

	_, body := b.get("/messages/" + conv + "/poll?after=0")
	if strings.Contains(body, `"length"`) {
		t.Errorf("a photograph came back with a duration: %s", body)
	}
}

// The sender sees what they just sent, without reloading the page.
//
// The composer renders the server's reply to the send. That reply was being
// built from the message row as it came back from the insert — which carried
// the attachment's id and not the attachment — so `file` was null, the bubble
// drew empty, and the only way to see the photograph you had just sent was to
// refresh by hand.
func TestTheSenderGetsTheFileBackOnTheSend(t *testing.T) {
	a := newApp(t)
	b := newAppSharing(t, a)
	conv := pair(t, a, b, "echoone", "echotwo")

	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	_ = form.WriteField("csrf_token", a.csrf())
	part, err := form.CreateFormFile("file", "dome.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(pngPixel); err != nil {
		t.Fatal(err)
	}
	form.Close()

	req, err := http.NewRequest(http.MethodPost, a.server.URL+"/messages/"+conv, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	// The headers the script sends, which is what asks for the reply as data
	// rather than as a redirect.
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "fetch")
	req.Header.Set("X-CSRF-Token", a.csrf())

	res, err := a.client.Do(req)
	if err != nil {
		t.Fatalf("sending: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("sending a picture → %d", res.StatusCode)
	}

	var reply struct {
		Messages []struct {
			File *struct {
				ID   string `json:"id"`
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"file"`
		} `json:"messages"`
	}
	raw, _ := io.ReadAll(res.Body)
	if err := json.Unmarshal(raw, &reply); err != nil {
		t.Fatalf("send json: %v (%s)", err, raw)
	}
	if len(reply.Messages) != 1 {
		t.Fatalf("the send returned %d messages, want 1: %s", len(reply.Messages), raw)
	}
	if reply.Messages[0].File == nil {
		t.Fatalf("the send came back without the file it carried: %s", raw)
	}
	if reply.Messages[0].File.Kind != "image" || reply.Messages[0].File.Name != "dome.png" {
		t.Errorf("the file came back as %+v, want the picture that was sent", reply.Messages[0].File)
	}
}
