package services

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"

	"pentagi/pkg/resources"
	"pentagi/pkg/server/response"
	"pentagi/pkg/skills"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// SkillsService exposes the skill store over REST. The store is nil when the
// embedding provider or the shared pgx pool is unavailable; read endpoints
// then return 503 instead of failing at query time.
type SkillsService struct {
	store     *skills.Store
	dataDir   string
	extractor skills.Extractor
}

func NewSkillsService(store *skills.Store, dataDir string, extractor skills.Extractor) *SkillsService {
	return &SkillsService{
		store:     store,
		dataDir:   dataDir,
		extractor: extractor,
	}
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

// ImportFile converts an uploaded document to markdown with the ingestion
// sidecar and imports the result as a skill.
//
// @Summary Import a skill from a document
// @Tags Skills
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param file formData file true "document to extract (markdown, text, html, pdf, docx, zip)"
// @Param kind formData string false "skill, reference or engagement"
// @Param status formData string false "draft, reviewed, canonical or deprecated"
// @Param visibility formData string false "private, shared or admin"
// @Param title formData string false "title override"
// @Param tags formData string false "comma-separated tags"
// @Param language formData string false "language code"
// @Param packId formData string false "pack identifier"
// @Success 201 {object} response.successResp{data=object}
// @Router /skills/import/file [post]
func (s *SkillsService) ImportFile(c *gin.Context) {
	if !s.available(c) {
		return
	}
	if s.extractor == nil || !s.extractor.IsAvailable() {
		response.Error(c, response.ErrSkillsIngestUnavailable, nil)
		return
	}

	ctx := c.Request.Context()
	uid := int64(c.GetUint64("uid"))

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, skills.MaxImportFileBytes)
	header, err := c.FormFile("file")
	if err != nil {
		response.Error(c, response.ErrKnowledgeInvalidRequest, fmt.Errorf("a file part is required: %w", err))
		return
	}

	file, err := header.Open()
	if err != nil {
		response.Error(c, response.ErrKnowledgeInvalidRequest, err)
		return
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, skills.MaxImportFileBytes+1))
	if err != nil {
		response.Error(c, response.ErrKnowledgeInvalidRequest, err)
		return
	}
	if int64(len(content)) > skills.MaxImportFileBytes {
		response.Error(c, response.ErrKnowledgeInvalidRequest,
			fmt.Errorf("the file exceeds %d bytes", skills.MaxImportFileBytes))
		return
	}

	filename := strings.TrimSpace(header.Filename)
	extraction, err := s.extractor.Extract(ctx, filename, content)
	if err != nil {
		s.writeStoreError(c, err, "extract document")
		return
	}

	doc, err := s.store.ImportMarkdown(ctx, uid, skills.ImportInput{
		Content:     extraction.Markdown,
		Title:       firstNonEmpty(c.PostForm("title"), extraction.Title),
		Kind:        c.PostForm("kind"),
		Visibility:  c.PostForm("visibility"),
		Status:      c.PostForm("status"),
		SourceKind:  "ingest",
		SourceURI:   filename,
		Language:    c.PostForm("language"),
		PackID:      stringPtr(c.PostForm("packId")),
		DefaultTags: splitTags(c.PostForm("tags")),
	})
	if err != nil {
		s.writeStoreError(c, err, "import extracted document")
		return
	}

	response.Success(c, http.StatusCreated, gin.H{"document": doc, "extraction": extraction})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func splitTags(value string) []string {
	parts := strings.Split(value, ",")
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			tags = append(tags, trimmed)
		}
	}
	return tags
}

func stringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
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

// ListFiles returns the attachments declared for a skill.
//
// @Summary List skill files
// @Tags Skills
// @Produce json
// @Security BearerAuth
// @Param idOrSlug path string true "document id or slug"
// @Success 200 {object} response.successResp{data=object}
// @Router /skills/{idOrSlug}/files [get]
func (s *SkillsService) ListFiles(c *gin.Context) {
	if !s.available(c) {
		return
	}

	uid := int64(c.GetUint64("uid"))
	doc, files, err := s.store.Read(c.Request.Context(), uid, c.Param("idOrSlug"))
	if err != nil {
		s.writeStoreError(c, err, "list skill files")
		return
	}

	response.Success(c, http.StatusOK, gin.H{"document_id": doc.ID, "files": files, "total": len(files)})
}

// UploadFiles stores one or more attachments for a skill. The bytes go to the
// shared resource blob store, keyed by MD5, and knowledge_files keeps the link.
//
// @Summary Upload skill files
// @Tags Skills
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param idOrSlug path string true "document id or slug"
// @Param path formData string false "virtual directory inside the skill"
// @Param files formData file true "files to upload"
// @Success 201 {object} response.successResp{data=object}
// @Router /skills/{idOrSlug}/files [post]
func (s *SkillsService) UploadFiles(c *gin.Context) {
	if !s.available(c) {
		return
	}

	ctx := c.Request.Context()
	uid := int64(c.GetUint64("uid"))

	doc, err := s.store.Get(ctx, uid, c.Param("idOrSlug"))
	if err != nil {
		s.writeStoreError(c, err, "get skill for upload")
		return
	}
	if doc.UserID != uid {
		s.writeStoreError(c, skills.ErrNotFound, "get skill for upload")
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, resources.MaxUploadRequestSize)
	multipartForm, err := c.MultipartForm()
	if err != nil {
		response.Error(c, response.ErrKnowledgeInvalidRequest, err)
		return
	}

	headers := multipartForm.File["files"]
	if len(headers) == 0 {
		if fh, uploadErr := c.FormFile("file"); uploadErr == nil && fh != nil {
			headers = append(headers, fh)
		}
	}
	if len(headers) == 0 {
		response.Error(c, response.ErrKnowledgeInvalidRequest, errors.New("at least one file is required"))
		return
	}
	if len(headers) > resources.MaxUploadFiles {
		response.Error(c, response.ErrKnowledgeInvalidRequest,
			fmt.Errorf("too many files: %d exceeds the limit of %d", len(headers), resources.MaxUploadFiles))
		return
	}

	dir, err := resources.SanitizeResourceDir(c.PostForm("path"))
	if err != nil {
		response.Error(c, response.ErrKnowledgeInvalidRequest, err)
		return
	}
	if err := resources.EnsureResourcesDir(s.dataDir); err != nil {
		response.Error(c, response.ErrInternal, err)
		return
	}

	blobsDir := resources.ResourcesDir(s.dataDir)
	uploaded := make([]skills.File, 0, len(headers))

	for _, header := range headers {
		file, err := s.uploadOne(c, uid, doc.ID, dir, blobsDir, header)
		if err != nil {
			response.Error(c, response.ErrKnowledgeInvalidRequest, err)
			return
		}
		uploaded = append(uploaded, *file)
	}

	response.Success(c, http.StatusCreated, gin.H{"files": uploaded, "total": len(uploaded)})
}

func (s *SkillsService) uploadOne(c *gin.Context, uid int64, documentID, dir, blobsDir string, header *multipart.FileHeader) (*skills.File, error) {
	name, err := resources.SanitizeResourceFileName(header.Filename)
	if err != nil {
		return nil, err
	}
	if header.Size > resources.MaxUploadFileSize {
		return nil, fmt.Errorf("file %q size %d bytes exceeds the limit of %d bytes",
			name, header.Size, resources.MaxUploadFileSize)
	}

	src, err := header.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open uploaded file %q: %w", name, err)
	}
	tmpPath, hash, size, saveErr := resources.SaveToTemp(src, blobsDir)
	src.Close()
	if saveErr != nil {
		return nil, saveErr
	}

	digest, err := fileSHA256(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("failed to hash uploaded file %q: %w", name, err)
	}

	if err := resources.CommitBlob(s.dataDir, hash, tmpPath); err != nil {
		os.Remove(tmpPath)
		return nil, err
	}

	file, err := s.store.AddFile(c.Request.Context(), uid, documentID, skills.File{
		Path:    resources.FilePath(dir, name),
		MIME:    header.Header.Get("Content-Type"),
		Size:    size,
		SHA256:  digest,
		BlobRef: hash,
	})
	if err != nil {
		if referenced, refErr := s.store.BlobReferenced(c.Request.Context(), hash); refErr == nil && !referenced {
			_ = resources.DeleteBlob(s.dataDir, hash)
		}
		return nil, err
	}

	return file, nil
}

// DownloadFile streams one attachment back to the caller.
//
// @Summary Download a skill file
// @Tags Skills
// @Security BearerAuth
// @Param idOrSlug path string true "document id or slug"
// @Param path path string true "virtual file path"
// @Success 200 {file} file
// @Router /skills/{idOrSlug}/files/{path} [get]
func (s *SkillsService) DownloadFile(c *gin.Context) {
	if !s.available(c) {
		return
	}

	uid := int64(c.GetUint64("uid"))
	filePath, err := resources.SanitizeResourcePath(strings.TrimPrefix(c.Param("path"), "/"))
	if err != nil {
		response.Error(c, response.ErrKnowledgeInvalidRequest, err)
		return
	}

	file, err := s.store.GetFile(c.Request.Context(), uid, c.Param("idOrSlug"), filePath)
	if err != nil {
		s.writeStoreError(c, err, "get skill file")
		return
	}

	blobPath := resources.BlobPath(s.dataDir, file.BlobRef)
	if _, err := os.Stat(blobPath); err != nil {
		response.Error(c, response.ErrKnowledgeNotFound, fmt.Errorf("file blob is missing"))
		return
	}

	c.FileAttachment(blobPath, file.Path)
}

// DeleteFile removes one attachment and its blob when it is no longer shared.
//
// @Summary Delete a skill file
// @Tags Skills
// @Produce json
// @Security BearerAuth
// @Param idOrSlug path string true "document id or slug"
// @Param path path string true "virtual file path"
// @Success 200 {object} response.successResp
// @Router /skills/{idOrSlug}/files/{path} [delete]
func (s *SkillsService) DeleteFile(c *gin.Context) {
	if !s.available(c) {
		return
	}

	ctx := c.Request.Context()
	uid := int64(c.GetUint64("uid"))
	filePath, err := resources.SanitizeResourcePath(strings.TrimPrefix(c.Param("path"), "/"))
	if err != nil {
		response.Error(c, response.ErrKnowledgeInvalidRequest, err)
		return
	}

	blobRef, err := s.store.DeleteFile(ctx, uid, c.Param("idOrSlug"), filePath)
	if err != nil {
		s.writeStoreError(c, err, "delete skill file")
		return
	}

	if referenced, err := s.store.BlobReferenced(ctx, blobRef); err == nil && !referenced {
		_ = resources.DeleteBlob(s.dataDir, blobRef)
	}

	response.Success(c, http.StatusOK, gin.H{"message": "file deleted"})
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Versions lists the stored revisions of a document, newest first.
//
// @Summary List skill versions
// @Tags Skills
// @Produce json
// @Security BearerAuth
// @Param idOrSlug path string true "document id or slug"
// @Success 200 {object} response.successResp{data=object}
// @Router /skills/{idOrSlug}/versions [get]
func (s *SkillsService) Versions(c *gin.Context) {
	if !s.available(c) {
		return
	}

	uid := int64(c.GetUint64("uid"))
	versions, err := s.store.ListVersions(c.Request.Context(), uid, c.Param("idOrSlug"))
	if err != nil {
		s.writeStoreError(c, err, "list skill versions")
		return
	}

	response.Success(c, http.StatusOK, gin.H{"versions": versions, "total": len(versions)})
}

// Version returns one revision of a document, body included.
//
// @Summary Read one skill version
// @Tags Skills
// @Produce json
// @Security BearerAuth
// @Param idOrSlug path string true "document id or slug"
// @Param version path int true "version number"
// @Success 200 {object} response.successResp{data=skills.DocumentVersion}
// @Router /skills/{idOrSlug}/versions/{version} [get]
func (s *SkillsService) Version(c *gin.Context) {
	if !s.available(c) {
		return
	}

	version, err := strconv.Atoi(c.Param("version"))
	if err != nil || version <= 0 {
		response.Error(c, response.ErrKnowledgeInvalidRequest, errors.New("the version must be a positive integer"))
		return
	}

	uid := int64(c.GetUint64("uid"))
	revision, err := s.store.GetVersion(c.Request.Context(), uid, c.Param("idOrSlug"), version)
	if err != nil {
		s.writeStoreError(c, err, "read skill version")
		return
	}

	response.Success(c, http.StatusOK, revision)
}

// Diff compares two revisions of a document.
//
// @Summary Diff skill versions
// @Tags Skills
// @Produce json
// @Security BearerAuth
// @Param idOrSlug path string true "document id or slug"
// @Param from query int true "source version"
// @Param to query int false "target version, defaults to the current one"
// @Success 200 {object} response.successResp{data=skills.VersionDiff}
// @Router /skills/{idOrSlug}/diff [get]
func (s *SkillsService) Diff(c *gin.Context) {
	if !s.available(c) {
		return
	}

	from, err := strconv.Atoi(c.Query("from"))
	if err != nil || from <= 0 {
		response.Error(c, response.ErrKnowledgeInvalidRequest, errors.New("the from query parameter is required"))
		return
	}
	to, _ := strconv.Atoi(c.Query("to"))

	uid := int64(c.GetUint64("uid"))
	diff, err := s.store.DiffVersions(c.Request.Context(), uid, c.Param("idOrSlug"), from, to)
	if err != nil {
		s.writeStoreError(c, err, "diff skill versions")
		return
	}

	response.Success(c, http.StatusOK, diff)
}

type skillPublishRequest struct {
	Status string `json:"status" binding:"required"`
	Note   string `json:"note"`
}

// Publish moves a skill through the review lifecycle.
//
// @Summary Publish a skill
// @Tags Skills
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param idOrSlug path string true "document id or slug"
// @Param json body skillPublishRequest true "target status and review note"
// @Success 200 {object} response.successResp{data=skills.Document}
// @Router /skills/{idOrSlug}/publish [post]
func (s *SkillsService) Publish(c *gin.Context) {
	if !s.available(c) {
		return
	}

	var req skillPublishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, response.ErrKnowledgeInvalidRequest, err)
		return
	}

	uid := int64(c.GetUint64("uid"))
	doc, err := s.store.Publish(c.Request.Context(), uid, c.Param("idOrSlug"), req.Status, req.Note)
	if err != nil {
		s.writeStoreError(c, err, "publish skill")
		return
	}

	response.Success(c, http.StatusOK, doc)
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
