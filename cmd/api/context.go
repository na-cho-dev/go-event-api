package main

import (
	"go-event-api/internal/database"

	"github.com/gin-gonic/gin"
)

func (app *application) GetUserFromContext(ctx *gin.Context) *database.User {
	contextUser, exist := ctx.Get("user")
	if !exist {
		return &database.User{}
	}
	user, ok := contextUser.(*database.User)
	if !ok {
		return &database.User{}
	}

	return user
}