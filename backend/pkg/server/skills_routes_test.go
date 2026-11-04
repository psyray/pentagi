package router

import (
	"testing"

	"pentagi/pkg/server/services"

	"github.com/gin-gonic/gin"
)

// The skills group mixes a static file collection with a catch-all download
// route. Gin panics at registration time when two patterns conflict, so this
// test fails loudly before a deployment discovers it at boot.
func TestSetSkillsGroupRoutesDoNotConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	setSkillsGroup(engine.Group("/api/v1"), (*services.SkillsService)(nil))
}
