// Package apiutil holds the JSON envelope shared by every /api/v1 handler:
//
//	{ "data": … }                                   success
//	{ "data": [...], "page": {page, per_page, total} } paged list
//	{ "error": { "code", "message", "fields" } }     failure
package apiutil

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"siraj/internal/platform/apperr"
)

type PageInfo struct {
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
	Total   int `json:"total"`
}

func OK(c *gin.Context, data any) { c.JSON(http.StatusOK, gin.H{"data": data}) }

func Created(c *gin.Context, data any) { c.JSON(http.StatusCreated, gin.H{"data": data}) }

func NoContent(c *gin.Context) { c.Status(http.StatusNoContent) }

func Paged(c *gin.Context, data any, p PageInfo) {
	c.JSON(http.StatusOK, gin.H{"data": data, "page": p})
}

// Fail writes an error. Unknown errors become a 500 and are logged, never shown.
func Fail(c *gin.Context, err error) {
	if e, ok := apperr.As(err); ok {
		c.AbortWithStatusJSON(e.Status, gin.H{"error": e})
		return
	}
	slog.ErrorContext(c.Request.Context(), "request failed", "path", c.FullPath(), "err", err)
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": apperr.New(500, "internal", "Something went wrong")})
}

// PageParams reads ?page= and ?per_page= with sane bounds.
func PageParams(c *gin.Context, defPer, maxPer int) (page, per int) {
	page, _ = strconv.Atoi(c.Query("page"))
	per, _ = strconv.Atoi(c.Query("per_page"))
	if page < 1 {
		page = 1
	}
	if per < 1 {
		per = defPer
	}
	if per > maxPer {
		per = maxPer
	}
	return page, per
}

// Lang is the request language chosen by the Lang middleware (ar | en | fr).
func Lang(c *gin.Context) string { return c.GetString("lang") }

// IDParam parses a numeric path parameter (domains and categories use bigint ids).
func IDParam(c *gin.Context, name string) (int64, error) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.BadRequest("bad_id", name+" must be a positive number")
	}
	return id, nil
}

// WantsJSON reports whether the client asked for JSON rather than a page: every
// /api/v1 route, an Accept header without text/html, or an htmx-free XHR.
func WantsJSON(c *gin.Context) bool {
	if strings.HasPrefix(c.Request.URL.Path, "/api/") {
		return true
	}
	accept := c.GetHeader("Accept")
	if strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/html") {
		return true
	}
	return false
}

// UUIDParam parses a uuid path parameter (games, challenges, rooms, messages).
func UUIDParam(c *gin.Context, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		return uuid.Nil, apperr.BadRequest("bad_id", name+" must be a uuid")
	}
	return id, nil
}

// BadJSON turns a binding error into the standard 400 the clients expect.
func BadJSON(err error) error {
	return apperr.BadRequest("bad_json", "The request body is not valid JSON: "+err.Error())
}
