package services

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"pentagi/pkg/server/response"
	"pentagi/pkg/skills"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// SkillsService exposes the skill store over REST. The store is nil when the
// embedding provider or the shared pgx pool is unavailable; read endpoints
// then return 503 instead of failing at query time.
type SkillsService struct {
	store *skills.Store
}

func NewSkillsService(store *skills.Store) *SkillsService {
	return &SkillsService{store: store}
}

func (s *SkillsService) available(c *gin.Context) bool {
	if s.store == nil || !s.store.IsAvailable() {
		response.Error(c, response.ErrKnowledgeStoreUnavail, errors.New("skills store is not configured"))
		return false
	}
	return true
}

// List returns the caller's skills plus shared reviewed/canonical ones.
//
// @Summary List skills
// @Tags Skills
// @Produce json
// @Security BearerAuth
// @Param kind query string false "skill, reference or mission"
// @Param status query string false "draft, reviewed, canonical or deprecated"
// @Param tags query string false "comma-separated tags"
// @Param limit query int false "maximum results (default 100, max 500)"
// @Success 200 {object} response.successResp{data=object}
// @Router /skills/ [get]
func (s *SkillsService) List(c *gin.Context) {
	if !s.available(c) {
		return
	}

	uid := int64(c.GetUint64("uid"))
	filter := skills.Filter{
		Kinds:    splitCSV(c.Query("kind")),
		Statuses: splitCSV(c.Query("status")),
		Tags:     splitCSV(c.Query("tags")),
		Limit:    queryInt(c, "limit"),
	}

	documents, err := s.store.List(c.Request.Context(), uid, filter)
	if err != nil {
		skillsLogger(c).WithError(err).Error("error listing skills")
		response.Error(c, response.ErrInternal, err)
		return
	}

	response.Success(c, http.StatusOK, gin.H{"documents": documents, "total": len(documents)})
}

// Get returns one skill or reference, with its attachments.
//
// @Summary Get a skill
// @Tags Skills
// @Produce json
// @Security BearerAuth
// @Param idOrSlug path string true "document id or slug"
// @Success 200 {object} response.successResp{data=object}
// @Router /skills/{idOrSlug} [get]
func (s *SkillsService) Get(c *gin.Context) {
	if !s.available(c) {
		return
	}

	uid := int64(c.GetUint64("uid"))
	doc, files, err := s.store.Read(c.Request.Context(), uid, c.Param("idOrSlug"))
	if err != nil {
		s.writeStoreError(c, err, "get skill")
		return
	}

	response.Success(c, http.StatusOK, gin.H{"document": doc, "files": files})
}

type skillImportRequest struct {
	Content    string   `json:"content" binding:"required"`
	Title      string   `json:"title"`
	Kind       string   `json:"kind"`
	Visibility string   `json:"visibility"`
	Status     string   `json:"status"`
	SourceURI  string   `json:"source_uri"`
	Language   string   `json:"language"`
	PackID     *string  `json:"pack_id"`
	Tags       []string `json:"tags"`
}

// Import parses a markdown skill, chunks it and embeds every chunk.
//
// @Summary Import a markdown skill
// @Tags Skills
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param json body skillImportRequest true "markdown content and metadata"
// @Success 201 {object} response.successResp{data=skills.Document}
// @Router /skills/import [post]
func (s *SkillsService) Import(c *gin.Context) {
	if !s.available(c) {
		return
	}

	var req skillImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, response.ErrKnowledgeInvalidRequest, err)
		return
	}

	uid := int64(c.GetUint64("uid"))
	doc, err := s.store.ImportMarkdown(c.Request.Context(), uid, skills.ImportInput{
		Content:     req.Content,
		Title:       req.Title,
		Kind:        req.Kind,
		Visibility:  req.Visibility,
		Status:      req.Status,
		SourceKind:  "api",
		SourceURI:   req.SourceURI,
		Language:    req.Language,
		PackID:      req.PackID,
		DefaultTags: req.Tags,
	})
	if err != nil {
		s.writeStoreError(c, err, "import skill")
		return
	}

	response.Success(c, http.StatusCreated, doc)
}

type skillSearchRequest struct {
	Query  string   `json:"query" binding:"required"`
	Kind   string   `json:"kind"`
	Status string   `json:"status"`
	Tags   []string `json:"tags"`
	Limit  int      `json:"limit"`
}

// Search runs the hybrid skill search.
//
// @Summary Search skills
// @Tags Skills
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param json body skillSearchRequest true "search request"
// @Success 200 {object} response.successResp{data=object}
// @Router /skills/search [post]
func (s *SkillsService) Search(c *gin.Context) {
	if !s.available(c) {
		return
	}

	var req skillSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, response.ErrKnowledgeInvalidRequest, err)
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		response.Error(c, response.ErrKnowledgeInvalidRequest, errors.New("query is required"))
		return
	}

	uid := int64(c.GetUint64("uid"))
	filter := skills.Filter{
		Kinds:    splitCSV(req.Kind),
		Statuses: splitCSV(req.Status),
		Tags:     req.Tags,
		Limit:    req.Limit,
	}
	hits, err := s.store.Search(c.Request.Context(), uid, req.Query, filter, req.Limit)
	if err != nil {
		s.writeStoreError(c, err, "search skills")
		return
	}

	response.Success(c, http.StatusOK, gin.H{"hits": hits, "total": len(hits)})
}

// Delete soft-deletes a skill and removes its chunks.
//
// @Summary Delete a skill
// @Tags Skills
// @Produce json
// @Security BearerAuth
// @Param idOrSlug path string true "document id or slug"
// @Success 200 {object} response.successResp
// @Router /skills/{idOrSlug} [delete]
func (s *SkillsService) Delete(c *gin.Context) {
	if !s.available(c) {
		return
	}

	uid := int64(c.GetUint64("uid"))
	if err := s.store.Delete(c.Request.Context(), uid, c.Param("idOrSlug")); err != nil {
		s.writeStoreError(c, err, "delete skill")
		return
	}

	response.Success(c, http.StatusOK, gin.H{"message": "skill deleted"})
}

func (s *SkillsService) writeStoreError(c *gin.Context, err error, action string) {
	switch {
	case errors.Is(err, skills.ErrNotFound):
		response.Error(c, response.ErrKnowledgeNotFound, err)
	case errors.Is(err, skills.ErrInvalidInput):
		response.Error(c, response.ErrKnowledgeInvalidRequest, err)
	case errors.Is(err, skills.ErrEmbedderUnavailable):
		response.Error(c, response.ErrKnowledgeStoreUnavail, err)
	default:
		skillsLogger(c).WithError(err).Errorf("error on %s", action)
		response.Error(c, response.ErrInternal, err)
	}
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func queryInt(c *gin.Context, name string) int {
	value, err := strconv.Atoi(c.Query(name))
	if err != nil {
		return 0
	}
	return value
}

func skillsLogger(c *gin.Context) *logrus.Entry {
	return logrus.WithContext(c.Request.Context())
}
