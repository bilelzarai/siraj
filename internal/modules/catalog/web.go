package catalog

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"siraj/internal/db/sqlc"
	"siraj/internal/http/render"
	"siraj/internal/platform/apperr"
	"siraj/internal/view"
	"siraj/web/templates/components"
	"siraj/web/templates/pages"
)

// Web holds the screens built on the catalog: Home, the Play hub, a domain's page and
// the Explore / Add / Manage lists. Everything respects Your domains (DM-03) except
// Explore, Add and onboarding, which are the only screens listing every domain (DM-04).
type Web struct {
	svc    *Service
	home   HomeExtras
	domain DomainExtras
}

// HomeExtras is what Home needs from the modules built later (question of the day,
// progress, challenges). Until they exist, the zero value leaves those sections out.
type HomeExtras interface {
	Home(c *gin.Context, v *view.Ctx, data *pages.HomeData)
}

// DomainExtras is what a domain's page needs from the progress module: how many
// players it had this week and the top of its weekly board (DM-06, LB-01).
type DomainExtras interface {
	Domain(c *gin.Context, v *view.Ctx, data *pages.DomainPageData)
}

func NewWeb(s *Service, extras HomeExtras) *Web { return &Web{svc: s, home: extras} }

// SetDomainExtras joins the progress module to the domain page after both exist.
func (w *Web) SetDomainExtras(e DomainExtras) { w.domain = e }

func (w *Web) RegisterWeb(public, private *gin.RouterGroup) {
	private.GET("/home", w.homePage)
	private.GET("/play", w.playPage)
	private.GET("/domains", w.explore)
	private.GET("/domains/:id", w.domainPage)
	private.POST("/domains/:id/add", w.addDomain)
	private.POST("/domains/:id/remove", w.removeDomain)
	// Explore is open to signed-out visitors too, so the welcome screen can link to it.
	public.GET("/explore", w.exploreVisitor)
}

const perPage = 6 // DM-08: big lists page 6 at a time

// slotsPerPage is the "Your domains" grid: two rows of three cards (HM-05).
const slotsPerPage = 6

// domainPage is one page of that grid. The flagship fills two slots and the
// "Add a domain" card takes the last slot of the last page, so a page is never
// three rows tall.
type domainPage struct {
	Domains []view.Domain
	Add     bool
}

// packDomains lays the player's domains out flagship first, then fills pages slot
// by slot. Counting cards instead of slots is what makes the grid spill into a
// third row, so the width of each card is what is added up here.
func packDomains(domains []view.Domain, featured *view.Domain, hasRest bool) []domainPage {
	items := make([]view.Domain, 0, len(domains))
	if featured != nil {
		items = append(items, *featured)
	}
	for _, d := range domains {
		if featured != nil && d.ID == featured.ID {
			continue
		}
		items = append(items, d)
	}

	grid := make([]domainPage, 0, 2)
	cur, used := domainPage{}, 0
	place := func(width int, add func(*domainPage)) {
		if used+width > slotsPerPage {
			grid = append(grid, cur)
			cur, used = domainPage{}, 0
		}
		add(&cur)
		used += width
	}
	for i := range items {
		width := 1
		if featured != nil && items[i].ID == featured.ID {
			width = 2
		}
		place(width, func(p *domainPage) { p.Domains = append(p.Domains, items[i]) })
	}
	if hasRest {
		place(1, func(p *domainPage) { p.Add = true })
	}
	if len(cur.Domains) > 0 || cur.Add || len(grid) == 0 {
		grid = append(grid, cur)
	}
	return grid
}

func (w *Web) homePage(c *gin.Context) {
	v := view.From(c)
	data := pages.HomeData{Goal: 5}
	// The "Your domains" cards, flagship first, paged 6 slots at a time (HM-05).
	domains := v.MyDomains
	for i := range domains {
		if domains[i].IsFeatured {
			data.Featured = &domains[i]
			break
		}
	}
	// The catalog the player can actually reach: Islamic-only hides the general
	// domains everywhere, including this count (DM-09).
	if _, total, err := w.svc.ListDomains(c.Request.Context(), ListInput{
		Who: v.User, Scope: ScopeAll, Lang: v.Lang, Page: 1, PerPage: 1,
	}); err == nil {
		data.TotalDomains = total
		data.RestDomains = total - len(domains)
		if data.RestDomains < 0 {
			data.RestDomains = 0
		}
	}

	// The cards are packed into slots, not counted: the grid is two rows of three
	// and the flagship fills two of them (HM-05).
	grid := packDomains(domains, data.Featured, data.RestDomains > 0)
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	if page > len(grid) {
		page = len(grid)
	}
	data.Domains, data.AddCard = grid[page-1].Domains, grid[page-1].Add
	data.Page, data.Pages = page, len(grid)

	// Category chips for the visible cards.
	ids := make([]int64, 0, len(data.Domains))
	for _, d := range data.Domains {
		ids = append(ids, d.ID)
	}
	byDomain, err := w.categoriesFor(c, v.Lang, ids)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	data.Categories = byDomain

	data.RamadanHref = w.ramadanHref(c, v)

	if w.home != nil {
		w.home.Home(c, v, &data)
	}
	if data.StreakLine == "" {
		if v.IsGuest() {
			data.StreakLine = v.Tr("home.guest_start_line")
		} else {
			data.StreakLine = v.Tr("home.streak.none")
		}
	}
	render.Page(c, pages.Home(v, data))
}

// islamicSlug and ramadanSlug are the catalog's own names for the flagship domain and
// its fasting topic; the Ramadan countdown looks them up instead of trusting a label a
// translator can change.
const (
	islamicFamily = "islamic"
	ramadanSlug   = "ramadan"
	// practiseQuestions is the smallest game there is (games.QuickGameQuestions);
	// catalog cannot import games, which already imports it.
	practiseQuestions = 5
)

// ramadanHref points "Practise Ramadan & Fasting" at the topic it names: the Ramadan
// category of the player's Islamic domain. It only links to the topic when the bank
// can fill a game (GP-02); otherwise the domain page, which lists everything there is
// to practise. A player without an Islamic domain is sent to the catalog to add one.
func (w *Web) ramadanHref(c *gin.Context, v *view.Ctx) string {
	var islamic *view.Domain
	for i := range v.MyDomains {
		if v.MyDomains[i].Family == islamicFamily {
			islamic = &v.MyDomains[i]
			break
		}
	}
	if islamic == nil {
		return "/domains"
	}
	cats, err := w.svc.CategoriesOf(c.Request.Context(), v.Lang, []int64{islamic.ID})
	if err == nil {
		for _, cat := range cats[islamic.ID] {
			if cat.Slug == ramadanSlug && int(cat.QuestionCount) >= practiseQuestions {
				return fmt.Sprintf("/setup?domain=%d&view=%d&category=%d&count=%d",
					islamic.ID, islamic.ID, cat.ID, practiseQuestions)
			}
		}
	}
	return fmt.Sprintf("/domains/%d", islamic.ID)
}

func (w *Web) playPage(c *gin.Context) {
	v := view.From(c)
	domain := w.currentDomain(v, c.Query("domain"))
	data := pages.PlayData{Domain: domain}
	if domain != nil {
		cats, err := w.categoriesFor(c, v.Lang, []int64{domain.ID})
		if err != nil {
			w.fail(c, v, err)
			return
		}
		data.Categories = cats[domain.ID]
	}
	render.Page(c, pages.Play(v.WithFocus(domain), data))
}

func (w *Web) domainPage(c *gin.Context) {
	v := view.From(c)
	id, err := idParam(c)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	row, err := w.svc.GetDomain(c.Request.Context(), v.User, id, v.Lang)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	domain := ViewDomain(row)
	cats, err := w.categoriesFor(c, v.Lang, []int64{id})
	if err != nil {
		w.fail(c, v, err)
		return
	}
	// "More domains": the player's other domains first, then anything else published.
	more, _, err := w.svc.ListDomains(c.Request.Context(), ListInput{
		Who: v.User, Scope: ScopeAll, Lang: v.Lang, Page: 1, PerPage: 7,
	})
	if err != nil {
		w.fail(c, v, err)
		return
	}
	others := make([]view.Domain, 0, 6)
	for _, other := range ViewDomains(more) {
		if other.ID != id && len(others) < 6 {
			others = append(others, other)
		}
	}
	data := pages.DomainPageData{Domain: domain, Categories: cats[id], More: others}
	if w.domain != nil {
		w.domain.Domain(c, v, &data)
	}
	render.Page(c, pages.DomainPage(v.WithFocus(&domain), data))
}

func (w *Web) explore(c *gin.Context) {
	v := view.From(c)
	mode := sheetMode(c.Query("ns"), "explore")
	// Manage is a dialog over the screen that opened it (O03) and nothing else: a
	// visit without htmx goes to Settings, where Your domains lives.
	if mode == "manage" && !render.IsHTMX(c) {
		render.Redirect(c, manageHome)
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	data, err := w.exploreData(c, v, mode, c.Query("q"), page)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	// O01–O03 open over the screen that asked for them; a plain visit still gets the
	// whole page, so the three screens work without JavaScript.
	if c.GetHeader("HX-Request") != "" {
		if mode == "manage" {
			render.Page(c, pages.ManageDomainsModal(v, data))
		} else {
			render.Page(c, pages.DomainSheet(v, data))
		}
		return
	}
	render.Page(c, pages.Explore(v, data))
}

// manageHome is where Manage sends a player who has no htmx: Settings carries Your
// domains with the same two buttons.
const manageHome = "/settings"

// sheetMode keeps the three namespaces of the domain screens apart: Explore (O01),
// Add to your domains (O02) and Manage (O03).
func sheetMode(raw, fallback string) string {
	switch raw {
	case "add", "manage":
		return raw
	}
	return fallback
}

// exploreData is one page of the catalog as O01, O02 and O03 all need it; the three
// share a list, and only Manage narrows it to the player's own domains (DM-05, DM-08).
func (w *Web) exploreData(c *gin.Context, v *view.Ctx, mode, query string, page int) (pages.ExploreData, error) {
	if page < 1 {
		page = 1
	}
	in := ListInput{Who: v.User, Scope: ScopeAll, Q: query, Lang: v.Lang, Page: page, PerPage: perPage, Alpha: true}
	// On "Add to your domains" the ones already on the home come first, so a player
	// can see what they picked without paging back for it (O02).
	in.MineFirst = mode == "add"
	if mode == "manage" {
		in.Scope = ScopeMine
		in.PerPage = MaxUserDomains
	}
	rows, total, err := w.svc.ListDomains(c.Request.Context(), in)
	if err != nil {
		return pages.ExploreData{}, err
	}
	pageCount := (total + in.PerPage - 1) / in.PerPage
	if pageCount < 1 {
		pageCount = 1
	}
	data := pages.ExploreData{
		Mode: mode, Domains: ViewDomains(rows), Query: query,
		Page: page, Pages: pageCount, Total: total, CanRemove: len(v.MyDomains) > 1,
	}
	data.From = (page-1)*in.PerPage + 1
	data.To = min(page*in.PerPage, total)
	if total == 0 {
		data.From = 0
	}
	if n, err := w.svc.db.Q.CountPublishedCategories(c.Request.Context()); err == nil {
		data.CategoryCount = int(n)
	}
	return data, nil
}

// exploreVisitor lets a signed-out visitor browse the catalog from the welcome screen.
func (w *Web) exploreVisitor(c *gin.Context) {
	v := view.From(c)
	if v.SignedIn() {
		render.Redirect(c, "/domains")
		return
	}
	rows, total, err := w.svc.ListDomains(c.Request.Context(), ListInput{
		Scope: ScopeAll, Q: c.Query("q"), Lang: v.Lang, Page: 1, PerPage: perPage,
	})
	if err != nil {
		w.fail(c, v, err)
		return
	}
	render.Page(c, pages.Explore(v, pages.ExploreData{
		Mode: "explore", Domains: ViewDomains(rows), Query: c.Query("q"), Page: 1,
		Pages: (total + perPage - 1) / perPage, Total: total,
	}))
}

func (w *Web) addDomain(c *gin.Context) {
	v := view.From(c)
	id, err := idParam(c)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if _, err := w.svc.AddMyDomain(c.Request.Context(), v.User, id, sqlc.UserDomainSourceAdded); err != nil {
		w.flashBack(c, v, err)
		return
	}
	if c.PostForm("then") == "sheet" {
		w.sheetAgain(c, v, "add")
		return
	}
	render.Redirect(c, backTo(c, "/home"))
}

func (w *Web) removeDomain(c *gin.Context) {
	v := view.From(c)
	id, err := idParam(c)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	if err := w.svc.RemoveMyDomain(c.Request.Context(), v.User, id); err != nil {
		w.flashBack(c, v, err)
		return
	}
	if c.PostForm("then") == "sheet" {
		w.sheetAgain(c, v, "manage")
		return
	}
	render.Redirect(c, backTo(c, manageHome))
}

// sheetAgain answers a card tapped inside O02 or O03: the drawer swaps its own cards
// and stays where it was, on the same search and the same page. Only a visit without
// htmx still travels to the whole screen (DM-04).
func (w *Web) sheetAgain(c *gin.Context, v *view.Ctx, fallback string) {
	mode := sheetMode(c.PostForm("ns"), fallback)
	if !render.IsHTMX(c) {
		if mode == "manage" {
			render.Redirect(c, manageHome)
			return
		}
		render.Redirect(c, "/domains?ns="+mode)
		return
	}
	// The shell read Your domains before this write, so read them again: the ticks,
	// the count and the "keep at least one" lock all follow the new list.
	if rows, err := w.svc.MyDomains(c.Request.Context(), v.User, v.Lang); err == nil {
		v.MyDomains = ViewDomains(rows)
	}
	page, _ := strconv.Atoi(c.PostForm("page"))
	data, err := w.exploreData(c, v, mode, c.PostForm("q"), page)
	if err != nil {
		w.fail(c, v, err)
		return
	}
	data.Changed = true
	if mode == "manage" {
		render.Page(c, pages.ManageDomainsModal(v, data))
		return
	}
	render.Page(c, pages.DomainSheetToggled(v, data))
}

// categoriesFor loads the categories of several domains at once, keyed by domain.
func (w *Web) categoriesFor(c *gin.Context, lang string, domainIDs []int64) (map[int64][]components.Category, error) {
	return w.svc.CategoriesOf(c.Request.Context(), lang, domainIDs)
}

// currentDomain is the domain in focus: the one asked for when it is the player's,
// otherwise their first domain (DM-03 keeps every screen inside Your domains).
func (w *Web) currentDomain(v *view.Ctx, raw string) *view.Domain {
	if raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
			for i := range v.MyDomains {
				if v.MyDomains[i].ID == id {
					return &v.MyDomains[i]
				}
			}
		}
	}
	for i := range v.MyDomains {
		if v.MyDomains[i].IsFeatured {
			return &v.MyDomains[i]
		}
	}
	if len(v.MyDomains) > 0 {
		return &v.MyDomains[0]
	}
	return nil
}

func (w *Web) fail(c *gin.Context, v *view.Ctx, err error) {
	if e, ok := apperr.As(err); ok && e.Status == http.StatusNotFound {
		render.Status(c, http.StatusNotFound, pages.NotFound(v))
		return
	}
	_ = c.Error(err)
	render.Status(c, http.StatusInternalServerError, pages.ServerError(v))
}

// flashBack sends the player back where they were with the service's message on top.
func (w *Web) flashBack(c *gin.Context, v *view.Ctx, err error) {
	message := v.Tr("error.body")
	status := http.StatusInternalServerError
	if e, ok := apperr.As(err); ok {
		status = e.Status
		message = e.Message
		if e.Code == "last_domain" {
			message = v.Tr("domains.last_one")
		}
	} else {
		_ = c.Error(err)
	}
	render.Toast(c, message)
	if render.IsHTMX(c) {
		c.Status(http.StatusNoContent)
		return
	}
	withFlash := v.Clone()
	withFlash.Flash = message
	render.Status(c, status, pages.Explore(withFlash, pages.ExploreData{
		Mode: "explore", Domains: v.MyDomains, Page: 1, Pages: 1, CanRemove: len(v.MyDomains) > 1,
	}))
}

func idParam(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.NotFound("domain")
	}
	return id, nil
}

func backTo(c *gin.Context, fallback string) string {
	back := c.PostForm("back")
	if back == "" || back[0] != '/' {
		return fallback
	}
	return back
}
