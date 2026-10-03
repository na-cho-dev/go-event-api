package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/na-cho-dev/go-event-api/internal/database"
)

// eventExists writes a 404 and returns false when the event is missing.
func (s *Server) eventExists(c *gin.Context, id int64) bool {
	if _, err := s.stores.Events.Get(c.Request.Context(), id); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			fail(c, http.StatusNotFound, CodeNotFound, "The event does not exist.")
			return false
		}
		s.internalError(c, "load event", err)
		return false
	}
	return true
}

func (s *Server) addAttendance(c *gin.Context, eventID, userID int64) {
	attendee, err := s.stores.Attendees.Add(c.Request.Context(), eventID, userID)
	if err != nil {
		switch {
		case errors.Is(err, database.ErrDuplicate):
			fail(c, http.StatusConflict, CodeConflict, "This user already attends the event.")
		case errors.Is(err, database.ErrInvalidReference):
			fail(c, http.StatusNotFound, CodeNotFound, "The event or user does not exist.")
		default:
			s.internalError(c, "add attendee", err)
		}
		return
	}
	c.JSON(http.StatusCreated, attendee)
}

func (s *Server) removeAttendance(c *gin.Context, eventID, userID int64) {
	if err := s.stores.Attendees.Remove(c.Request.Context(), eventID, userID); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			fail(c, http.StatusNotFound, CodeNotFound, "This user does not attend the event.")
			return
		}
		s.internalError(c, "remove attendee", err)
		return
	}
	c.Status(http.StatusNoContent)
}

// listAttendees returns the users attending an event.
//
//	@Summary	List attendees
//	@Tags		Attendees
//	@Produce	json
//	@Param		id	path		int	true	"Event ID"
//	@Success	200	{object}	CollectionResponse[database.User]
//	@Failure	404	{object}	ErrorResponse
//	@Router		/api/v1/events/{id}/attendees [get]
func (s *Server) listAttendees(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok || !s.eventExists(c, id) {
		return
	}
	users, err := s.stores.Attendees.ListUsersForEvent(c.Request.Context(), id)
	if err != nil {
		s.internalError(c, "list attendees", err)
		return
	}
	c.JSON(http.StatusOK, CollectionResponse[database.User]{Data: users})
}

// joinEvent registers the caller for an event.
//
//	@Summary	Join an event
//	@Tags		Attendees
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		int	true	"Event ID"
//	@Success	201	{object}	database.Attendee
//	@Failure	401	{object}	ErrorResponse
//	@Failure	404	{object}	ErrorResponse
//	@Failure	409	{object}	ErrorResponse
//	@Router		/api/v1/events/{id}/attendees [post]
func (s *Server) joinEvent(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok || !s.eventExists(c, id) {
		return
	}
	s.addAttendance(c, id, currentUser(c).ID)
}

// leaveEvent removes the caller from an event.
//
//	@Summary	Leave an event
//	@Tags		Attendees
//	@Security	BearerAuth
//	@Param		id	path	int	true	"Event ID"
//	@Success	204
//	@Failure	401	{object}	ErrorResponse
//	@Failure	404	{object}	ErrorResponse
//	@Router		/api/v1/events/{id}/attendees [delete]
func (s *Server) leaveEvent(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok || !s.eventExists(c, id) {
		return
	}
	s.removeAttendance(c, id, currentUser(c).ID)
}

// addAttendee lets the owner register another user.
//
//	@Summary	Add an attendee
//	@Tags		Attendees
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path		int	true	"Event ID"
//	@Param		userId	path		int	true	"User ID"
//	@Success	201		{object}	database.Attendee
//	@Failure	401		{object}	ErrorResponse
//	@Failure	403		{object}	ErrorResponse
//	@Failure	404		{object}	ErrorResponse
//	@Failure	409		{object}	ErrorResponse
//	@Router		/api/v1/events/{id}/attendees/{userId} [post]
func (s *Server) addAttendee(c *gin.Context) {
	eventID, ok := pathID(c, "id")
	if !ok {
		return
	}
	userID, ok := pathID(c, "userId")
	if !ok {
		return
	}
	if s.loadOwnedEvent(c, eventID, "add attendees") == nil {
		return
	}
	if _, err := s.stores.Users.Get(c.Request.Context(), userID); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			fail(c, http.StatusNotFound, CodeNotFound, "The user does not exist.")
			return
		}
		s.internalError(c, "load user", err)
		return
	}
	s.addAttendance(c, eventID, userID)
}

// removeAttendee lets the owner unregister a user.
//
//	@Summary	Remove an attendee
//	@Tags		Attendees
//	@Security	BearerAuth
//	@Param		id		path	int	true	"Event ID"
//	@Param		userId	path	int	true	"User ID"
//	@Success	204
//	@Failure	401	{object}	ErrorResponse
//	@Failure	403	{object}	ErrorResponse
//	@Failure	404	{object}	ErrorResponse
//	@Router		/api/v1/events/{id}/attendees/{userId} [delete]
func (s *Server) removeAttendee(c *gin.Context) {
	eventID, ok := pathID(c, "id")
	if !ok {
		return
	}
	userID, ok := pathID(c, "userId")
	if !ok {
		return
	}
	if s.loadOwnedEvent(c, eventID, "remove attendees") == nil {
		return
	}
	s.removeAttendance(c, eventID, userID)
}

// listEventsForUser returns the events a user attends.
//
//	@Summary	Events for an attendee
//	@Tags		Attendees
//	@Produce	json
//	@Param		id	path		int	true	"User ID"
//	@Success	200	{object}	CollectionResponse[database.Event]
//	@Failure	404	{object}	ErrorResponse
//	@Router		/api/v1/attendees/{id}/events [get]
func (s *Server) listEventsForUser(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if _, err := s.stores.Users.Get(c.Request.Context(), id); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			fail(c, http.StatusNotFound, CodeNotFound, "The user does not exist.")
			return
		}
		s.internalError(c, "load user", err)
		return
	}
	events, err := s.stores.Attendees.ListEventsForUser(c.Request.Context(), id)
	if err != nil {
		s.internalError(c, "list events for user", err)
		return
	}
	c.JSON(http.StatusOK, CollectionResponse[database.Event]{Data: events})
}
