package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/na-cho-dev/go-event-api/internal/database"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
	maxQueryLength  = 100
)

// EventRequest creates or replaces an event.
type EventRequest struct {
	Name        string    `json:"name" binding:"required,min=3,max=200" example:"Lagos Backend Meetup"`
	Description string    `json:"description" binding:"required,min=10,max=5000" example:"An evening of talks on APIs, queues and databases."`
	Date        time.Time `json:"date" binding:"required" example:"2026-11-05T18:00:00Z"`
	Location    string    `json:"location" binding:"required,min=3,max=200" example:"Yaba, Lagos"`
}

// loadOwnedEvent fetches an event and checks the caller owns it. It writes
// the error response and returns nil when the caller may not proceed.
func (s *Server) loadOwnedEvent(c *gin.Context, id int64, action string) *database.Event {
	event, err := s.stores.Events.Get(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			fail(c, http.StatusNotFound, CodeNotFound, "The event does not exist.")
			return nil
		}
		s.internalError(c, "load event", err)
		return nil
	}
	if event.OwnerID != currentUser(c).ID {
		fail(c, http.StatusForbidden, CodeForbidden, "Only the event owner can "+action+".")
		return nil
	}
	return event
}

// listEvents returns a page of events.
//
//	@Summary	List events
//	@Description	Paged, ordered by date. Filter with q (name or location), owner (user id) and from (RFC 3339).
//	@Tags		Events
//	@Produce	json
//	@Param		page	query		int		false	"Page number, starting at 1"	default(1)
//	@Param		limit	query		int		false	"Page size, 1 to 100"		default(20)
//	@Param		q		query		string	false	"Match name or location"
//	@Param		owner	query		int		false	"Only events owned by this user"
//	@Param		from	query		string	false	"Only events on or after this RFC 3339 instant"
//	@Success	200		{object}	ListResponse[database.Event]
//	@Failure	400		{object}	ErrorResponse
//	@Router		/api/v1/events [get]
func (s *Server) listEvents(c *gin.Context) {
	filter := database.EventFilter{Page: 1, Limit: defaultPageSize}
	var details []FieldError

	if v := c.Query("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			details = append(details, FieldError{Field: "page", Message: "must be an integer of at least 1"})
		} else {
			filter.Page = n
		}
	}
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxPageSize {
			details = append(details, FieldError{Field: "limit", Message: "must be an integer between 1 and 100"})
		} else {
			filter.Limit = n
		}
	}
	if v := strings.TrimSpace(c.Query("q")); v != "" {
		if len(v) > maxQueryLength {
			details = append(details, FieldError{Field: "q", Message: "must be at most 100 characters"})
		} else {
			filter.Query = v
		}
	}
	if v := c.Query("owner"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			details = append(details, FieldError{Field: "owner", Message: "must be a positive integer"})
		} else {
			filter.OwnerID = &n
		}
	}
	if v := c.Query("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			details = append(details, FieldError{Field: "from", Message: "must be an RFC 3339 timestamp"})
		} else {
			filter.From = &t
		}
	}
	if len(details) > 0 {
		fail(c, http.StatusBadRequest, CodeValidation, "One or more query parameters are invalid.", details...)
		return
	}

	events, total, err := s.stores.Events.List(c.Request.Context(), filter)
	if err != nil {
		s.internalError(c, "list events", err)
		return
	}
	c.JSON(http.StatusOK, ListResponse[database.Event]{
		Data: events,
		Meta: newPageMeta(filter.Page, filter.Limit, total),
	})
}

// getEvent returns one event.
//
//	@Summary	Get an event
//	@Tags		Events
//	@Produce	json
//	@Param		id	path		int	true	"Event ID"
//	@Success	200	{object}	database.Event
//	@Failure	404	{object}	ErrorResponse
//	@Router		/api/v1/events/{id} [get]
func (s *Server) getEvent(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	event, err := s.stores.Events.Get(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			fail(c, http.StatusNotFound, CodeNotFound, "The event does not exist.")
			return
		}
		s.internalError(c, "load event", err)
		return
	}
	c.JSON(http.StatusOK, event)
}

// createEvent creates an event owned by the caller.
//
//	@Summary	Create an event
//	@Tags		Events
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body	body		EventRequest	true	"Event"
//	@Success	201		{object}	database.Event
//	@Failure	400		{object}	ErrorResponse
//	@Failure	401		{object}	ErrorResponse
//	@Router		/api/v1/events [post]
func (s *Server) createEvent(c *gin.Context) {
	var req EventRequest
	if !bindJSON(c, &req) {
		return
	}
	event := &database.Event{
		OwnerID:     currentUser(c).ID,
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Date:        req.Date.UTC(),
		Location:    strings.TrimSpace(req.Location),
	}
	if err := s.stores.Events.Insert(c.Request.Context(), event); err != nil {
		s.internalError(c, "insert event", err)
		return
	}
	c.JSON(http.StatusCreated, event)
}

// updateEvent replaces an event's details.
//
//	@Summary	Update an event
//	@Tags		Events
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path		int				true	"Event ID"
//	@Param		body	body		EventRequest	true	"Event"
//	@Success	200		{object}	database.Event
//	@Failure	400		{object}	ErrorResponse
//	@Failure	401		{object}	ErrorResponse
//	@Failure	403		{object}	ErrorResponse
//	@Failure	404		{object}	ErrorResponse
//	@Router		/api/v1/events/{id} [put]
func (s *Server) updateEvent(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	event := s.loadOwnedEvent(c, id, "update it")
	if event == nil {
		return
	}
	var req EventRequest
	if !bindJSON(c, &req) {
		return
	}

	event.Name = strings.TrimSpace(req.Name)
	event.Description = strings.TrimSpace(req.Description)
	event.Date = req.Date.UTC()
	event.Location = strings.TrimSpace(req.Location)
	if err := s.stores.Events.Update(c.Request.Context(), event); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			fail(c, http.StatusNotFound, CodeNotFound, "The event does not exist.")
			return
		}
		s.internalError(c, "update event", err)
		return
	}
	c.JSON(http.StatusOK, event)
}

// deleteEvent removes an event and its attendees.
//
//	@Summary	Delete an event
//	@Tags		Events
//	@Security	BearerAuth
//	@Param		id	path	int	true	"Event ID"
//	@Success	204
//	@Failure	401	{object}	ErrorResponse
//	@Failure	403	{object}	ErrorResponse
//	@Failure	404	{object}	ErrorResponse
//	@Router		/api/v1/events/{id} [delete]
func (s *Server) deleteEvent(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if s.loadOwnedEvent(c, id, "delete it") == nil {
		return
	}
	if err := s.stores.Events.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			fail(c, http.StatusNotFound, CodeNotFound, "The event does not exist.")
			return
		}
		s.internalError(c, "delete event", err)
		return
	}
	c.Status(http.StatusNoContent)
}
