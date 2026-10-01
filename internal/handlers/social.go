package handlers

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
	"github.com/bilelzarai/siraj/internal/service"
	"github.com/bilelzarai/siraj/internal/views"
)

// ---------------------------------------------------------------- friends --

func (h *Handlers) Friends(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	ctx := r.Context()

	tab := r.URL.Query().Get("tab")
	if tab != "requests" && tab != "search" {
		tab = "all"
	}

	d := views.FriendsData{Tab: tab, Query: strings.TrimSpace(r.URL.Query().Get("q"))}

	friends, err := h.repo.Friends(ctx, c.User.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	d.Friends = friends

	switch tab {
	case "requests":
		if d.Incoming, err = h.repo.IncomingRequests(ctx, c.User.ID); err != nil {
			h.serverError(w, r, err)
			return
		}
		if d.Outgoing, err = h.repo.OutgoingRequests(ctx, c.User.ID); err != nil {
			h.serverError(w, r, err)
			return
		}
	case "search":
		if d.Query != "" {
			if d.Results, err = h.repo.SearchUsers(ctx, c.User.ID, d.Query, 25); err != nil {
				h.serverError(w, r, err)
				return
			}
		}
	}

	h.render(w, r, http.StatusOK, views.Friends(c, d))
}

// FriendAction handles request / accept / decline / remove from one route,
// keyed by the {action} path segment.
func (h *Handlers) FriendAction(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	if h.tooManyWrites(w, r, service.LimitFriend) {
		return
	}
	action := chi.URLParam(r, "action")

	username := strings.TrimSpace(r.PostFormValue("username"))
	other, err := h.repo.UserByUsername(r.Context(), username)
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}
	if other.ID == c.User.ID {
		h.forbidden(w, r)
		return
	}

	switch action {
	case "request":
		err = h.social.SendFriendRequest(r.Context(), c.User.ID, other.ID)
	case "accept":
		err = h.social.RespondFriendRequest(r.Context(), other.ID, c.User.ID, true)
	case "decline":
		err = h.social.RespondFriendRequest(r.Context(), other.ID, c.User.ID, false)
	case "remove":
		err = h.social.RemoveFriend(r.Context(), c.User.ID, other.ID)
	case "block":
		if err = h.social.Block(r.Context(), c.User.ID, other.ID); err == nil {
			h.flash(w, "success", c.T("friends.blocked", other.DisplayName))
		}
	case "unblock":
		if err = h.social.Unblock(r.Context(), c.User.ID, other.ID); err == nil {
			h.flash(w, "success", c.T("friends.unblocked", other.DisplayName))
		}
	default:
		h.NotFound(w, r)
		return
	}

	if err != nil && !errors.Is(err, repository.ErrConflict) {
		h.serverError(w, r, err)
		return
	}
	redirect(w, r, backTo(r, "/friends"))
}

// -------------------------------------------------------------- profiles --

func (h *Handlers) MyProfile(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	redirect(w, r, "/u/"+user.Username)
}

func (h *Handlers) Profile(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	ctx := r.Context()

	profile, err := h.repo.UserByUsername(ctx, chi.URLParam(r, "username"))
	if err != nil {
		h.notFoundOrError(w, r, err)
		return
	}
	// A temporary player has no public profile. They are a seat at somebody's
	// device for an evening, not a person you can visit, befriend or block —
	// and the generated username is the only thing that could lead here.
	if profile.IsGuest() {
		h.NotFound(w, r)
		return
	}

	stats, err := h.repo.Stats(ctx, profile.ID, c.Locale)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	d := views.ProfileData{
		Profile: profile,
		Stats:   stats,
		IsSelf:  profile.ID == c.User.ID,
	}
	if badges, err := h.repo.Badges(ctx, profile.ID, c.Locale); err == nil {
		d.Badges = badges
	}
	if recent, err := h.repo.RecentActivity(ctx, profile.ID, c.Locale, 5); err == nil {
		d.Recent = recent
	}
	if rel, err := h.repo.Relation(ctx, c.User.ID, profile.ID); err == nil {
		d.Relation = rel
	}
	if rank, err := h.repo.MyRank(ctx, profile.ID, "global"); err == nil {
		d.Rank = rank
	}

	h.render(w, r, http.StatusOK, views.Profile(c, d))
}

func (h *Handlers) EditProfileForm(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	h.render(w, r, http.StatusOK, views.EditProfile(c, views.EditProfileData{User: c.User}))
}

func (h *Handlers) EditProfile(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	displayName := strings.TrimSpace(r.PostFormValue("display_name"))
	if len([]rune(displayName)) < 2 {
		h.render(w, r, http.StatusUnprocessableEntity, views.EditProfile(c, views.EditProfileData{
			User:   c.User,
			Errors: map[string]string{"display_name": c.T("auth.error.displayNameShort")},
		}))
		return
	}

	bio := clip(strings.TrimSpace(r.PostFormValue("bio")), 280)
	country := clip(strings.TrimSpace(r.PostFormValue("country")), 60)

	if err := h.repo.UpdateProfile(r.Context(), c.User.ID, displayName, bio, country); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.flash(w, "success", c.T("profile.saved"))
	redirect(w, r, "/profile")
}

// UploadAvatar replaces the generated gradient with a picture.
//
// The reference is stored in avatar_seed, which every query that draws a
// person already selects — see views.AvatarStyle for why that column carries
// two kinds of value.
func (h *Handlers) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	file, header, err := r.FormFile("photo")
	if err != nil {
		h.flash(w, "error", c.T("upload.empty"))
		redirect(w, r, "/profile/edit")
		return
	}
	defer file.Close()

	att, err := h.uploads.Store(r.Context(), c.User.ID, header.Filename, file)
	if err != nil {
		h.uploadFailed(w, r, err)
		return
	}
	// A profile picture is a picture. A PDF as an avatar renders as nothing at
	// all, which reads as the upload having silently failed.
	if att.Kind != models.AttachmentImage {
		h.flash(w, "error", c.T("upload.notAPicture"))
		redirect(w, r, "/profile/edit")
		return
	}

	if err := h.repo.SetAvatarSeed(r.Context(), c.User.ID, views.AvatarPhotoPrefix+att.ID.String()); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.flash(w, "success", c.T("profile.photoSaved"))
	redirect(w, r, "/profile")
}

// RemoveAvatar goes back to the generated gradient.
func (h *Handlers) RemoveAvatar(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	if err := h.repo.SetAvatarSeed(r.Context(), c.User.ID, ""); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.flash(w, "success", c.T("profile.photoRemoved"))
	redirect(w, r, "/profile/edit")
}

// ----------------------------------------------------------- leaderboard --

// LeaderboardSize is how many places the board shows. Your own rank is reported
// separately, so somebody outside the ten still learns where they stand.
const LeaderboardSize = 10

func (h *Handlers) Leaderboard(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	scope := r.URL.Query().Get("scope")
	if scope != "friends" {
		scope = "global"
	}

	// Ten. A leaderboard is a thing you glance at to see whether you are on it;
	// fifty rows is a directory, and nobody reads to the end of one.
	entries, err := h.repo.Leaderboard(r.Context(), c.User.ID, scope, LeaderboardSize)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	// The rank shown has to be the rank on the board being shown.
	rank, _ := h.repo.MyRank(r.Context(), c.User.ID, scope)

	h.render(w, r, http.StatusOK, views.Leaderboard(c, views.LeaderboardData{
		Scope:   scope,
		Entries: entries,
		MyRank:  rank,
	}))
}

// ------------------------------------------------------------- challenges --

func (h *Handlers) Challenges(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	ctx := r.Context()

	tab := r.URL.Query().Get("tab")
	switch tab {
	case "outgoing", "finished":
	case "new":
		// There was a tab here that listed people with a Challenge button
		// beside each — a second, thinner copy of the screen that actually
		// sets a round up, reachable from the same page by a different press.
		// Old links land on the real one.
		redirect(w, r, "/challenges/new")
		return
	default:
		tab = "incoming"
	}

	d := views.ChallengesData{Tab: tab}
	var err error

	// The device panel belongs here only while it does something.
	//
	// It was shown whenever this account had guests at all, which on a screen
	// listing challenges is a row of names with no stated purpose — pressing
	// one moved a seat that mattered to nothing, and the page then announced
	// that somebody was playing when nobody was. Adding and removing guests is
	// part of setting a round up, and lives on the screen that does that; here
	// it appears only when a match on this device is actually under way, where
	// it means "whose turn — hand it over".
	if seats := h.localPlayerData(r); seats.Playing {
		d.Seats = seats
	}

	switch tab {
	case "outgoing":
		d.Outgoing, err = h.repo.ChallengesFor(ctx, c.User.ID, "outgoing", c.Locale, 40)
	case "finished":
		d.Finished, err = h.repo.ChallengesFor(ctx, c.User.ID, "finished", c.Locale, 40)
	default:
		d.Incoming, err = h.repo.ChallengesFor(ctx, c.User.ID, "incoming", c.Locale, 40)
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	h.render(w, r, http.StatusOK, views.Challenges(c, d))
}

// NewChallengeForm is the round setup screen with an invitation list on it.
//
// `?opponent=` pre-ticks somebody — that is the path from a profile or the
// friends list — and arriving without one is equally valid: the picker is how
// you choose, and a match takes one person or nine.
func (h *Handlers) NewChallengeForm(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	d, err := h.setupData(r, c.Locale, 0, 0, service.DefaultQuestions)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	friends, err := h.repo.Friends(r.Context(), c.User.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	d.Friends = friends

	// The people in the room you are standing in are challengeable too, and
	// they are the only strangers who are. Loaded here so the picker offers
	// exactly the set the server will accept.
	if room, err := h.repo.CurrentRoom(r.Context(), c.User.ID); err == nil {
		d.Room = room
		if peers, err := h.repo.RoomPeers(r.Context(), c.User.ID, "", repository.RoomPeersShown, 0); err == nil {
			d.RoomPeers = peers
		}
	} else if !errors.Is(err, repository.ErrNotFound) {
		h.serverError(w, r, err)
		return
	}

	// And whoever is sitting at this device, who needs no invitation at all.
	d.Seats = h.localPlayerData(r)

	// Deliberately no redirect when the lists are empty.
	//
	// It used to bounce to friend search, which made a match on this device
	// impossible to set up from scratch: the way to add somebody to the device
	// is the panel on this very screen, and you were sent away from it before
	// you could reach it. The screen handles having nobody on it — each group
	// says what to do about being empty — and that is a better answer than a
	// redirect that contradicts one of the three ways in.
	// Questions this player has already written, offered as a source.
	if sets, err := h.repo.QuestionSets(r.Context(), c.User.ID); err == nil {
		d.Sets = sets
	}

	// Which group the link came from, so a room member arrives with the room
	// tab open rather than on Friends, where they are not listed and the
	// pre-ticked name has nothing to tick.
	if group := strings.TrimSpace(r.URL.Query().Get("group")); models.ValidSource(group) {
		d.PlayerSource = group
	}

	// One name, or several: the picker and the random draw both arrive here,
	// and the second of those has more than one person to tick.
	if names := strings.TrimSpace(r.URL.Query().Get("opponent")); names != "" {
		for _, username := range strings.Split(names, ",") {
			username = strings.TrimSpace(username)
			if username == "" {
				continue
			}
			opponent, err := h.repo.UserCardByUsername(r.Context(), username)
			if err != nil {
				h.notFoundOrError(w, r, err)
				return
			}
			d.Invited = append(d.Invited, opponent)
		}
		if len(d.Invited) > 0 {
			d.Opponent = d.Invited[0]
		}
	}

	// A rematch arrives with the same people already ticked.
	if id, ok := parseUUID(r.URL.Query().Get("rematch")); ok {
		if previous, err := h.repo.Challenge(r.Context(), id, c.Locale); err == nil {
			if previous.Player(c.User.ID) != nil {
				d.RematchOf = id.String()
				d.Invited = rematchInvitations(previous, c.User.ID)
			}
		}
	}

	h.render(w, r, http.StatusOK, views.Setup(c, d))
}

// authoredQuestions reads the questions the host wrote on the form.
//
// The fields arrive as parallel lists — prompt[], choice0[]…choice3[],
// correct[] — one entry per question, which is what a repeating fieldset
// submits. A blank prompt is an empty row somebody opened and did not fill in,
// not an error.
func authoredQuestions(r *http.Request) []service.AuthoredQuestion {
	out, _ := authoredRows(r)
	return out
}

// authoredRows reads every row, and reports which form row each one came from.
// The index matters: a blank row in the middle must not shift the rows after it
// onto somebody else's answers, and a refusal has to be able to point at the
// row that caused it.
func authoredRows(r *http.Request) ([]service.AuthoredQuestion, []int) {
	prompts := r.PostForm["q_prompt"]
	var out []service.AuthoredQuestion
	var rows []int

	// Walked by row so the index lines up with the other fields, and a blank
	// row in the middle does not shift the ones after it onto the wrong answers.
	for i, prompt := range prompts {
		if strings.TrimSpace(prompt) == "" {
			continue
		}
		q := service.AuthoredQuestion{
			Prompt:      prompt,
			Explanation: formAt(r, "q_explanation", i),
		}
		for choice := 0; choice < 4; choice++ {
			q.Choices = append(q.Choices, formAt(r, fmt.Sprintf("q_choice%d", choice), i))
		}
		// Each row has its own radio group. One shared name made the whole form
		// a single group — marking the answer in one question unmarked it in
		// every other, and the server read one value for all of them.
		if n, err := strconv.Atoi(r.PostFormValue(fmt.Sprintf("q_correct_%d", i))); err == nil {
			q.Correct = n
		}
		out = append(out, q)
		rows = append(rows, i)
	}
	return out, rows
}

// formAt reads the i-th value of a repeated field, or "" when the row did not
// submit one.
func formAt(r *http.Request, name string, i int) string {
	values := r.PostForm[name]
	if i >= len(values) {
		return ""
	}
	return values[i]
}

// rematchInvitations is everyone who played the previous match, except the
// person asking for the rematch.
func rematchInvitations(previous *models.Challenge, hostID uuid.UUID) []*models.UserCard {
	var out []*models.UserCard
	for _, p := range previous.Players {
		if p.UserID == hostID || p.Card == nil || p.State == models.PlayerDeclined {
			continue
		}
		out = append(out, p.Card)
	}
	return out
}

// CreateChallenge opens a match. One opponent or nine.
//
// The form submits `opponent` once per person invited, so the same screen makes
// a duel and a five-way without a mode switch: ticking one friend is what it
// always was.
func (h *Handlers) CreateChallenge(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	if h.tooManyWrites(w, r, service.LimitFriend) {
		return
	}

	var invited []uuid.UUID
	var cards []*models.UserCard
	for _, username := range r.PostForm["opponent"] {
		username = strings.TrimSpace(username)
		if username == "" {
			continue
		}
		card, err := h.repo.UserCardByUsername(r.Context(), username)
		if err != nil {
			h.notFoundOrError(w, r, err)
			return
		}
		invited = append(invited, card.ID)
		cards = append(cards, card)
	}

	// Guests sitting at this device. Each one is checked against the person
	// signed in: a player id in a form is worth nothing on its own, and this is
	// the only place that can say whether it belongs to the sender.
	//
	// An id that does not belong here is refused rather than dropped. Silently
	// removing somebody from a match the sender thought they had built is how
	// a four-player game turns into a three-player game nobody asked for.
	var local []uuid.UUID
	var badPlayer bool
	for _, raw := range r.PostForm["local"] {
		id, ok := parseUUID(strings.TrimSpace(raw))
		if !ok {
			continue
		}
		player, err := h.repo.LocalPlayer(r.Context(), c.User.ID, id)
		if err != nil {
			badPlayer = true
			continue
		}
		local = append(local, player.ID)
	}

	// Which of the three groups this match is with. Taken from the form when
	// it says, and otherwise read from what was actually sent — but a form
	// carrying both kinds is a mixture, and that is refused rather than
	// resolved by guessing which half was meant.
	playerSource := strings.TrimSpace(r.PostFormValue("player_source"))
	if !models.ValidSource(playerSource) {
		playerSource = models.SourceFriends
		if len(local) > 0 && len(invited) == 0 {
			playerSource = models.SourceDevice
		}
	}

	// Which side everybody is on, for a match played in teams. The form posts
	// team[<username-or-id>]; anything missing falls to side one.
	format := r.PostFormValue("format")
	teams := map[uuid.UUID]int{}
	if format == models.FormatTeam {
		for i, id := range invited {
			teams[id] = intParam(r, "team_"+cards[i].Username, 1)
		}
		for _, id := range local {
			teams[id] = intParam(r, "team_"+id.String(), 2)
		}
	}

	category := intParam(r, "category", 0)
	difficulty := intParam(r, "difficulty", 0)
	count := intParam(r, "count", service.DefaultQuestions)

	var rematchOf *uuid.UUID
	if id, ok := parseUUID(r.PostFormValue("rematch_of")); ok {
		rematchOf = &id
	}

	// The source is chosen, not inferred. Working it out from what happened to
	// be filled in made "I picked a set and also typed in the write box" a
	// question with no honest answer.
	source := r.PostFormValue("source")
	opts := service.MatchOptions{
		CategoryID: nilIfZero(category),
		Difficulty: difficulty,
		Count:      count,
		Locale:     c.Locale,
		Message:    r.PostFormValue("message"),
		RematchOf:  rematchOf,
	}
	switch source {
	case "write":
		opts.Authored = authoredQuestions(r)
	case "set":
		if id, ok := parseUUID(r.PostFormValue("set")); ok {
			opts.SetID = &id
		}
	default:
		// The bank, which needs neither.
	}

	opts.Format = format
	opts.Teams = teams
	opts.HostTeam = intParam(r, "host_team", 1)
	opts.Local = local
	opts.PlayerSource = playerSource

	if badPlayer {
		h.challengeRefused(w, r, c, c.T("challenge.notYourPlayer"), category, difficulty, count, source, cards)
		return
	}

	// One press, one match. Without this a resent POST drew a second set of
	// questions and sent a second invitation to the same people.
	claimed, previous, err := h.claimRequest(r, c.User.ID, "challenge.create")
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if !claimed {
		if previous != "" {
			redirect(w, r, previous)
			return
		}
		redirect(w, r, "/challenges?tab=outgoing")
		return
	}

	ch, err := h.social.CreateMatch(r.Context(), c.User.ID, invited, opts)

	if err != nil {
		h.releaseRequest(r)

		// What went wrong, in the reader's language, before deciding where to
		// put it. The dialog and the full setup screen are two places to say
		// the same sentence, and working it out twice is how they drift.
		message := challengeProblem(c, err, cards, availabilityFor(h, r, c.Locale, category, difficulty))
		if message == "" {
			h.serverError(w, r, err)
			return
		}
		h.challengeRefused(w, r, c, message, category, difficulty, count, source, cards)
		return
	}
	h.completeRequest(r, "/challenges?tab=outgoing")
	slog.InfoContext(r.Context(), "match created",
		"id", ch.ID, "host", c.User.Username,
		"from", playerSource, "players", len(invited)+len(local)+1)

	// What to say about it, which is not the same sentence for all three
	// groups. Nothing was "sent" to a guest sitting at this keyboard: they are
	// already here, and the next thing to happen is the phone being passed.
	seated := len(invited) + len(local)
	message := c.T("challenge.sentTo", seated)
	where := "/challenges?tab=outgoing"
	if playerSource == models.SourceDevice {
		message = c.T("challenge.readyHere", seated)
		where = "/challenges"
	}

	// Only now is there anywhere to go. The dialog is told where; it was
	// deliberately not sent to the challenge screen before the challenge
	// existed.
	if isAPIRequest(r) {
		writeJSON(w, http.StatusOK, map[string]any{
			"url":     where,
			"players": seated,
			"message": message,
		})
		return
	}
	h.flash(w, "success", message)
	redirect(w, r, where)
}

// AcceptChallenge starts the viewer's round in a match, using its fixed set.
//
// A match needs somebody to play against: with only the host in, there is
// nothing to compare a score with, so it refuses rather than producing a
// scoreboard of one.
func (h *Handlers) AcceptChallenge(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)
	// Whoever is holding the device answers for themselves. On a shared phone
	// this is how the second and third players take their turn: the seat moves,
	// and accepting starts a round for that player rather than for the host.
	ch, actorID, ok := h.loadChallengeForViewer(w, r)
	if !ok {
		return
	}
	actor, err := h.repo.UserByID(r.Context(), actorID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	me := ch.Player(actor.ID)
	if me == nil {
		h.forbidden(w, r)
		return
	}
	if me.HasPlayed() {
		redirect(w, r, "/challenges?tab=finished")
		return
	}
	if ch.Over() {
		key := "challenge.closed"
		if ch.Status == models.ChallengeCancelled {
			key = "challenge.cancelledNotice"
		}
		h.flash(w, "info", c.T(key))
		redirect(w, r, "/challenges?tab=finished")
		return
	}

	// Joining is not playing. Accepting an invitation puts you in; the round
	// only starts once there are enough people for the result to mean anything.
	if me.State == models.PlayerInvited {
		if err := h.social.JoinMatch(r.Context(), ch.ID, actor.ID); err != nil {
			switch {
			case errors.Is(err, repository.ErrBusy):
				// Inside another match already. Several invitations can wait at
				// once; only one of them can be answered at a time.
				h.flash(w, "error", c.T("challenge.alreadyInOne"))
			case errors.Is(err, service.ErrMatchOver):
				// Cancelled or expired between the page being drawn and the
				// button being pressed. An ordinary race, and it used to
				// produce a server error page.
				h.flash(w, "info", c.T("challenge.cancelledNotice"))
			default:
				h.serverError(w, r, err)
				return
			}
			redirect(w, r, "/challenges")
			return
		}
		ch.Player(actor.ID).State = models.PlayerJoined
	}
	// Accepting is saying you are here, and that is all it is.
	//
	// It used to be the whole of starting: press it twice and your round
	// opened, whenever you happened to press it — so two people who had
	// arranged to play each other answered the same questions on different
	// days and the scoreboard compared two solitary rounds. The host sets the
	// match going now, once, for everybody at the same moment.
	if !ch.Started() {
		if ch.HostID == actor.ID {
			h.flash(w, "info", c.T("challenge.pressStart"))
		} else {
			h.flash(w, "info", c.T("challenge.waitingForStart"))
		}
		redirect(w, r, "/challenges")
		return
	}

	chID := ch.ID
	session, err := h.game.Start(r.Context(), actor.ID, service.StartOptions{
		CategoryID:  ch.CategoryID,
		Difficulty:  ch.Difficulty,
		Count:       len(ch.QuestionIDs),
		Locale:      c.Locale,
		Mode:        models.ModeChallenge,
		ChallengeID: &chID,
		QuestionIDs: ch.QuestionIDs,
	})
	if err != nil {
		h.serverError(w, r, err)
		return
	}

	if err := h.repo.AttachMatchSession(r.Context(), ch.ID, actor.ID, session.ID); err != nil {
		if errors.Is(err, repository.ErrConflict) || errors.Is(err, repository.ErrBusy) {
			_ = h.game.Abandon(r.Context(), session.ID, actor.ID)
			if errors.Is(err, repository.ErrBusy) {
				h.flash(w, "error", c.T("challenge.alreadyInOne"))
				redirect(w, r, "/challenges")
				return
			}
			h.flash(w, "info", c.T("challenge.closed"))
			redirect(w, r, "/challenges?tab=finished")
			return
		}
		h.serverError(w, r, err)
		return
	}

	redirect(w, r, "/play/round")
}

// StartChallenge is the host setting the match going for everybody.
//
// One press opens a round for every player who has accepted, at the same
// instant and on the same questions. Before it, accepting put people in a lobby
// and nothing else; after it, each of them has a round waiting and is told so.
func (h *Handlers) StartChallenge(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	ch, actorID, ok := h.loadChallengeForViewer(w, r)
	if !ok {
		return
	}

	players, err := h.social.StartMatch(r.Context(), ch, actorID)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrForbidden):
			h.flash(w, "error", c.T("challenge.hostOnly"))
		case errors.Is(err, service.ErrNotEnoughPlayers):
			h.flash(w, "info", c.T("challenge.needMore", models.MinChallengePlayers))
		case errors.Is(err, service.ErrMatchOver):
			h.flash(w, "info", c.T("challenge.cancelledNotice"))
		case errors.Is(err, service.ErrAlreadyStarted):
			// Somebody pressed it twice. Their round is open either way.
			redirect(w, r, "/play/round")
			return
		default:
			h.serverError(w, r, err)
			return
		}
		redirect(w, r, "/challenges")
		return
	}

	// A round each, in one go. The host's own is created here too, so the
	// press that starts the match is the press that starts their round.
	h.openRounds(w, r, ch, players)
	redirect(w, r, "/play/round")
}

// openRounds creates the round for every player the match started for.
//
// A failure for one person is logged rather than fatal: the match is under way
// and the others are playing it, and stopping everything because one round
// could not be written would be the worse answer.
func (h *Handlers) openRounds(w http.ResponseWriter, r *http.Request, ch *models.Challenge, players []uuid.UUID) {
	chID := ch.ID
	locale := localeFrom(r)

	for _, id := range players {
		if _, err := h.repo.RoundInMatch(r.Context(), id, chID, locale); err == nil {
			continue // already has one
		}
		session, err := h.game.Start(r.Context(), id, service.StartOptions{
			CategoryID:  ch.CategoryID,
			Difficulty:  ch.Difficulty,
			Count:       len(ch.QuestionIDs),
			Locale:      locale,
			Mode:        models.ModeChallenge,
			ChallengeID: &chID,
			QuestionIDs: ch.QuestionIDs,
		})
		if err != nil {
			slogError(r, err)
			continue
		}
		if err := h.repo.AttachMatchSession(r.Context(), chID, id, session.ID); err != nil {
			_ = h.game.Abandon(r.Context(), session.ID, id)
			slogError(r, err)
		}
	}
}

func (h *Handlers) DeclineChallenge(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	ch, actorID, ok := h.loadChallengeForViewer(w, r)
	if !ok {
		return
	}
	host := ch.Player(actorID) != nil && ch.HostID == actorID
	if err := h.social.DeclineChallenge(r.Context(), ch, actorID); err != nil {
		switch {
		case errors.Is(err, repository.ErrForbidden):
			h.forbidden(w, r)
		case errors.Is(err, service.ErrMatchOver):
			h.flash(w, "info", c.T("challenge.cancelledNotice"))
			redirect(w, r, "/challenges")
		default:
			h.serverError(w, r, err)
		}
		return
	}
	// The host saying no ends it for everybody, so it is reported as that
	// rather than as one person stepping out.
	if host {
		h.flash(w, "info", c.T("challenge.cancelledByYou"))
	} else {
		h.flash(w, "info", c.T("challenge.declined"))
	}
	redirect(w, r, "/challenges")
}

// CancelChallenge is the host calling their own match off.
//
// A separate button from Decline because they are separate acts: a guest
// declining steps out of a match that carries on, and a host declining ends it
// for four other people. One word for both was how a host ended everybody's
// match while believing they had only left it.
func (h *Handlers) CancelChallenge(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	ch, actorID, ok := h.loadChallengeForViewer(w, r)
	if !ok {
		return
	}
	if err := h.social.CancelMatch(r.Context(), ch, actorID); err != nil {
		switch {
		case errors.Is(err, repository.ErrForbidden):
			h.flash(w, "error", c.T("challenge.hostOnly"))
		case errors.Is(err, service.ErrMatchOver):
			h.flash(w, "info", c.T("challenge.cancelledNotice"))
		default:
			h.serverError(w, r, err)
			return
		}
		redirect(w, r, "/challenges")
		return
	}
	h.flash(w, "info", c.T("challenge.cancelledByYou"))
	redirect(w, r, "/challenges")
}

// RandomChallenge opens a match against somebody picked out of the room.
//
// The room is the whole pool. "Play someone random" over every account here
// would be a way for a stranger to be put in front of you without either of you
// choosing it; a room is a place people walk into on purpose, and walking out
// of it is how you stop being drawn.
func (h *Handlers) RandomChallenge(w http.ResponseWriter, r *http.Request) {
	c := h.viewCtx(w, r)

	if h.tooManyWrites(w, r, service.LimitFriend) {
		return
	}

	want := intParam(r, "players", 1)
	opponents, err := h.social.RandomOpponents(r.Context(), c.User.ID, want)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrNoRoom):
			h.flash(w, "info", c.T("rooms.joinFirst"))
			redirect(w, r, "/messages?tab=room")
		case errors.Is(err, service.ErrRoomEmpty):
			h.flash(w, "info", c.T("rooms.nobodyFree"))
			redirect(w, r, backTo(r, "/challenges/new"))
		default:
			h.serverError(w, r, err)
		}
		return
	}

	names := make([]string, 0, len(opponents))
	for _, o := range opponents {
		names = append(names, o.Username)
	}
	// Straight to the setup form with them already picked, rather than opening
	// a match on settings nobody chose.
	redirect(w, r, "/challenges/new?opponent="+urlEscape(strings.Join(names, ",")))
}

// loadChallengeForViewer fetches a match and works out who at this device is
// answering for it.
//
// Membership comes from the player rows, not from challenger_id and
// opponent_id. Those two columns name the host and the first person invited and
// nobody else — they are kept so a two-player match still reads correctly
// through the old shape — so checking against them told the third player in a
// match, and every guest at a shared device, that they were not allowed to
// accept or decline an invitation addressed to them. "Not allowed" on your own
// invitation.
//
// The id it returns is who the action is about: the player holding the seat if
// they are in this match, otherwise the account holder if they are. A device
// may answer for any of its own players, and for nobody else.
func (h *Handlers) loadChallengeForViewer(w http.ResponseWriter, r *http.Request) (*models.Challenge, uuid.UUID, bool) {
	id, ok := parseUUID(chi.URLParam(r, "id"))
	if !ok {
		h.NotFound(w, r)
		return nil, uuid.Nil, false
	}
	ch, err := h.repo.Challenge(r.Context(), id, localeFrom(r))
	if err != nil {
		h.notFoundOrError(w, r, err)
		return nil, uuid.Nil, false
	}

	// The same rule the card was drawn with, so the button answers for
	// whoever the card said it was about.
	//
	// This used to prefer the seat whenever the seated player was named on the
	// match at all — including when they had declined it. The screen had
	// already been taught to skip somebody who is out; the handler had not, so
	// a host pressing their own Cancel button was answered "only the host can
	// cancel a match".
	seat, account := uuid.Nil, uuid.Nil
	if actor := actorFrom(r); actor != nil {
		seat = actor.ID
	}
	if user := userFrom(r); user != nil {
		account = user.ID
	}
	if who := ch.WhoActs(seat, account); ch.HasPlayer(who) {
		return ch, who, true
	}
	h.forbidden(w, r)
	return nil, uuid.Nil, false
}

// ---------------------------------------------------------------- helpers --

func clip(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// backTo prefers the referring page so an action returns the user where they
// were, falling back to a known-safe path. The Referer is caller-controlled, so
// the validation lives in one place for every caller — see sameSiteReferer.
func backTo(r *http.Request, fallback string) string {
	return sameSiteReferer(r, fallback)
}

func slogError(r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "handler error", "path", r.URL.Path, "error", err)
}

// challengeProblem turns a refused match into one sentence in the reader's
// language, or "" for a fault that is nobody's mistake and belongs in the log.
//
// One function rather than a switch in each of the two places that report it:
// the dialog and the setup screen are two ways of asking the same question, and
// a reason that exists in only one of them is a reason somebody meets as a
// blank refusal.
func challengeProblem(c views.Ctx, err error, cards []*models.UserCard, available int) string {
	switch {
	case errors.Is(err, service.ErrNotFriends), errors.Is(err, service.ErrUnreachable):
		return c.T("challenge.notFriends")
	case errors.Is(err, service.ErrNotInRoom):
		return c.T("challenge.notInRoom")
	case errors.Is(err, service.ErrMixedSources):
		return c.T("challenge.oneGroup")
	case errors.Is(err, service.ErrInMatch):
		return c.T("challenge.alreadyInOne")
	case errors.Is(err, service.ErrOneSidedTeams):
		return c.T("challenge.twoSides")
	case errors.Is(err, service.ErrTeamsNeedMore):
		return c.T("challenge.teamsNeedMore", models.MinTeamPlayers)
	case errors.Is(err, service.ErrSelfTarget):
		return c.T("challenge.pickSomebody")
	case errors.Is(err, service.ErrTooManyPlayers):
		return c.T("challenge.tooMany", models.MaxChallengePlayers)
	case errors.Is(err, service.ErrDuelInFlight):
		if len(cards) > 0 {
			return c.T("challenge.waiting", cards[0].DisplayName)
		}
		return c.T("challenge.alreadyInOne")
	case errors.Is(err, service.ErrTooFewWritten):
		return c.T("challenge.writeAtLeast", service.MinQuestions)
	case errors.Is(err, service.ErrSetTooSmall):
		return c.T("challenge.setTooSmall", service.MinQuestions)
	case errors.Is(err, service.ErrBadQuestion):
		return c.T("challenge.questionIncomplete")
	case errors.Is(err, service.ErrTooManyQuestions):
		return c.T("challenge.tooManyQuestions", repository.MaxAuthoredPerMatch)
	case errors.Is(err, service.ErrNotEnoughQuestions):
		return c.T("game.noQuestions", available, service.MinQuestions)
	}
	return ""
}

// availabilityFor is how many questions the chosen category and difficulty can
// actually draw, for the one message that quotes the number.
func availabilityFor(h *Handlers, r *http.Request, locale string, category, difficulty int) int {
	counts, err := h.repo.AvailableCounts(r.Context(), locale)
	if err != nil {
		return 0
	}
	return counts.Count(category, difficulty)
}

// challengeRefused reports a match that could not be opened, in whichever form
// the caller asked for it.
//
// The dialog stays open on its error: the selection somebody has just made is
// still the selection they want, and throwing it away to render a page that
// says why is a punishment for being told no. The full screen re-renders with
// what was chosen still chosen, for the same reason.
func (h *Handlers) challengeRefused(w http.ResponseWriter, r *http.Request, c views.Ctx,
	message string, category, difficulty, count int, source string, cards []*models.UserCard) {

	if isAPIRequest(r) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": message})
		return
	}

	d, err := h.setupData(r, c.Locale, category, difficulty, count)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if len(cards) > 0 {
		d.Opponent = cards[0]
	}
	d.Invited = cards
	d.Source = source
	d.Error = message
	d.PlayerSource = strings.TrimSpace(r.PostFormValue("player_source"))
	if friends, err := h.repo.Friends(r.Context(), c.User.ID); err == nil {
		d.Friends = friends
	}
	d.Seats = h.localPlayerData(r)
	if room, err := h.repo.CurrentRoom(r.Context(), c.User.ID); err == nil {
		d.Room = room
		if peers, err := h.repo.RoomPeers(r.Context(), c.User.ID, "", repository.RoomPeersShown, 0); err == nil {
			d.RoomPeers = peers
		}
	}
	h.render(w, r, http.StatusUnprocessableEntity, views.Setup(c, d))
}
