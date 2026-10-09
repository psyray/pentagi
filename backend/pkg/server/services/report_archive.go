package services

import (
	"archive/tar"
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"pentagi/pkg/controller"
	"pentagi/pkg/database"
	"pentagi/pkg/docker"
	"pentagi/pkg/flowfiles"
	"pentagi/pkg/report"
	"pentagi/pkg/server/logger"
	"pentagi/pkg/server/response"

	"github.com/gin-gonic/gin"
)

// ReportArchiveService bundles one report into a downloadable archive: the
// assembled document plus the evidence its sections cited (plan § 6.10). File
// evidence lives in the flow's terminal container, screenshots on the host; a
// reference that cannot be resolved lands in annexes/MISSING.txt instead of
// failing the whole archive.
type ReportArchiveService struct {
	dataDir      string
	tenantPrefix string
	dockerClient docker.DockerClient
	reports      controller.ReportController
}

func NewReportArchiveService(
	dataDir string,
	tenantPrefix string,
	dockerClient docker.DockerClient,
	reports controller.ReportController,
) *ReportArchiveService {
	return &ReportArchiveService{
		dataDir:      dataDir,
		tenantPrefix: tenantPrefix,
		dockerClient: dockerClient,
		reports:      reports,
	}
}

// ReportBrandingLogo streams the report's branding logo. The reader renders it
// in an <img> (the session cookie authenticates) and the exporters fetch it as
// a blob; the archive bundles the same bytes as a file.
// @Summary Download the report branding logo
// @Description Streams the png/jpeg logo attached to a report's export branding.
// @Tags Reports
// @Produce image/png,image/jpeg,json
// @Security BearerAuth
// @Param flowID path int true "flow id" minimum(0)
// @Param reportID path int true "report id" minimum(0)
// @Success 200 {file} file "report branding logo"
// @Failure 403 {object} response.errorResp "reading the report not permitted"
// @Failure 404 {object} response.errorResp "report or logo not found"
// @Router /flows/{flowID}/reports/{reportID}/branding/logo [get]
func (s *ReportArchiveService) ReportBrandingLogo(c *gin.Context) {
	flowID, err := parseFlowIDParam(c)
	if err != nil {
		logger.FromContext(c).WithError(err).Error("error parsing flow id")
		response.Error(c, response.ErrFlowFilesInvalidRequest, err)
		return
	}

	reportID, err := strconv.ParseUint(c.Param("reportID"), 10, 64)
	if err != nil {
		logger.FromContext(c).WithError(err).Error("error parsing report id")
		response.Error(c, response.ErrFlowFilesInvalidRequest, err)
		return
	}

	privs := c.GetStringSlice("prm")
	if !slices.Contains(privs, "reports.read") && !slices.Contains(privs, "reports.admin") {
		response.Error(c, response.ErrNotPermitted, nil)
		return
	}

	uid := c.GetUint64("uid")
	ctx := c.Request.Context()

	name, mediaType, data, err := s.reports.BrandingLogo(ctx, int64(uid), int64(reportID))
	if err != nil {
		response.Error(c, response.ErrReportBrandingNotFound, err)
		return
	}

	row, err := s.reports.GetReport(ctx, int64(uid), int64(reportID))
	if err != nil || row.FlowID != int64(flowID) {
		// An ownership failure and a mismatched flow read the same: no logo here.
		response.Error(c, response.ErrNotPermitted, err)
		return
	}

	c.Header("Content-Disposition", fmt.Sprintf("inline; filename=%q", path.Base(name)))
	c.Data(http.StatusOK, mediaType, data)
}

// ArchiveReport downloads the report document and its cited evidence as a ZIP.
// @Summary Download a report document with its evidence
// @Description Bundles the assembled document with the container files and screenshots its sections cited.
// @Tags Reports
// @Produce application/zip,json
// @Security BearerAuth
// @Param flowID path int true "flow id" minimum(0)
// @Param reportID path int true "report id" minimum(0)
// @Success 200 {file} file "report archive (ZIP)"
// @Failure 403 {object} response.errorResp "downloading the report archive not permitted"
// @Failure 404 {object} response.errorResp "report not found"
// @Failure 500 {object} response.errorResp "internal error on building the report archive"
// @Router /flows/{flowID}/reports/{reportID}/archive [get]
func (s *ReportArchiveService) ArchiveReport(c *gin.Context) {
	flowID, err := parseFlowIDParam(c)
	if err != nil {
		logger.FromContext(c).WithError(err).Error("error parsing flow id")
		response.Error(c, response.ErrFlowFilesInvalidRequest, err)
		return
	}

	reportID, err := strconv.ParseUint(c.Param("reportID"), 10, 64)
	if err != nil {
		logger.FromContext(c).WithError(err).Error("error parsing report id")
		response.Error(c, response.ErrFlowFilesInvalidRequest, err)
		return
	}

	privs := c.GetStringSlice("prm")
	if !slices.Contains(privs, "reports.read") && !slices.Contains(privs, "reports.admin") {
		response.Error(c, response.ErrNotPermitted, nil)
		return
	}

	uid := c.GetUint64("uid")
	ctx := c.Request.Context()

	row, err := s.reports.GetReport(ctx, int64(uid), int64(reportID))
	if err != nil || row.FlowID != int64(flowID) {
		// An ownership failure and a mismatched flow read the same: no report here.
		response.Error(c, response.ErrNotPermitted, err)
		return
	}

	document, err := s.reports.Document(ctx, int64(uid), int64(reportID))
	if err != nil {
		logger.FromContext(c).WithError(err).WithField("report_id", reportID).Error("error assembling the report document")
		response.Error(c, response.ErrInternal, err)
		return
	}

	refs, err := s.reports.EvidenceRefs(ctx, int64(uid), int64(reportID))
	if err != nil {
		logger.FromContext(c).WithError(err).WithField("report_id", reportID).Error("error loading the report evidence manifest")
		response.Error(c, response.ErrInternal, err)
		return
	}

	attachments, err := s.reports.ListReportAttachments(ctx, int64(uid), int64(reportID))
	if err != nil {
		logger.FromContext(c).WithError(err).WithField("report_id", reportID).Error("error loading the report attachments")
		response.Error(c, response.ErrInternal, err)
		return
	}

	slug := reportArchiveSlug(row.Title, row.TemplateLabel)
	if err := streamZipArchive(c, slug+"_annexes.zip", func(w io.Writer) error {
		return s.buildArchive(ctx, w, row, slug, document, refs, attachments)
	}); err != nil {
		logger.FromContext(c).WithError(err).WithField("report_id", reportID).Error("error streaming the report archive")
	}
}

func (s *ReportArchiveService) buildArchive(
	ctx context.Context,
	w io.Writer,
	row *database.Report,
	slug string,
	document string,
	refs []report.EvidenceRef,
	attachments []database.ReportSectionAttachment,
) error {
	zw := zip.NewWriter(w)

	doc, err := zw.Create(slug + ".md")
	if err != nil {
		return err
	}
	if _, err := io.WriteString(doc, document); err != nil {
		return err
	}

	// The branding logo travels with the archive: the document cannot carry it
	// (no binary in markdown), so the bundle is where it is expected to live.
	if logoName, _, logoData, err := s.reports.BrandingLogo(ctx, row.UserID, row.ID); err == nil && len(logoData) > 0 {
		if entry, err := zw.Create(path.Join("annexes/branding", path.Base(logoName))); err == nil {
			_, _ = entry.Write(logoData)
		}
	}

	containerName := primaryContainerName(s.tenantPrefix, uint64(row.FlowID))
	missing := make([]string, 0)

	for _, ref := range refs {
		if parentRef(ref, refs) {
			continue
		}

		if ref.Kind == report.EvidenceKindScreenshot {
			if ref.ScreenshotName == "" {
				missing = append(missing, fmt.Sprintf("screenshot %d: no file name recorded", ref.ScreenshotID))
				continue
			}
			src := filepath.Join(s.dataDir, "screenshots", fmt.Sprintf("flow-%d", row.FlowID), ref.ScreenshotName)
			if err := addFileToZip(zw, path.Join("annexes/screenshots", ref.ScreenshotName), src); err != nil {
				missing = append(missing, fmt.Sprintf("screenshot %q: %v", ref.ScreenshotName, err))
			}
			continue
		}

		if ref.Path == "" {
			continue
		}

		reader, _, err := s.dockerClient.CopyFromContainer(ctx, containerName, ref.Path)
		if err != nil {
			missing = append(missing, fmt.Sprintf("file %q: %v", ref.Path, err))
			continue
		}
		count, err := addTarToZip(zw, reader, ref.Path, "annexes/files")
		reader.Close()
		if err != nil || count == 0 {
			missing = append(missing, fmt.Sprintf("file %q: %v", ref.Path, err))
		}
	}

	// Operator attachments (backlog v2, L6): a cache-relative path is read from
	// the host cache, an absolute one from the flow's terminal container.
	for _, att := range attachments {
		if err := s.addAttachmentToZip(ctx, zw, row.FlowID, att); err != nil {
			missing = append(missing, fmt.Sprintf("attachment %q: %v", att.Path, err))
		}
	}

	if len(missing) > 0 {
		entry, err := zw.Create("annexes/MISSING.txt")
		if err != nil {
			return err
		}
		if _, err := io.WriteString(entry, "These evidence references could not be bundled:\n\n"); err != nil {
			return err
		}
		for _, line := range missing {
			if _, err := io.WriteString(entry, "- "+line+"\n"); err != nil {
				return err
			}
		}
	}

	return zw.Close()
}

// addAttachmentToZip bundles one operator attachment. A cache-relative path
// (uploads/, resources/, container/) is read from the host cache; an absolute
// path is copied from the flow's terminal container.
func (s *ReportArchiveService) addAttachmentToZip(
	ctx context.Context,
	zw *zip.Writer,
	flowID int64,
	att database.ReportSectionAttachment,
) error {
	reqPath := strings.TrimSpace(att.Path)
	if reqPath == "" {
		return fmt.Errorf("no path recorded")
	}

	if strings.HasPrefix(reqPath, "/") {
		containerName := primaryContainerName(s.tenantPrefix, uint64(flowID))
		reader, _, err := s.dockerClient.CopyFromContainer(ctx, containerName, reqPath)
		if err != nil {
			return err
		}
		defer reader.Close()

		count, err := addTarToZip(zw, reader, reqPath, "annexes/attachments")
		if err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("no file at %q", reqPath)
		}
		return nil
	}

	abs, err := flowfiles.ResolveCachedPath(s.dataDir, uint64(flowID), reqPath)
	if err != nil {
		return err
	}
	return addFileToZip(zw, path.Join("annexes/attachments", filepath.ToSlash(reqPath)), abs)
}

// reportArchiveSlug builds a safe base name from the report title.
func reportArchiveSlug(title, fallback string) string {
	raw := strings.TrimSpace(title)
	if raw == "" {
		raw = strings.TrimSpace(fallback)
	}
	if raw == "" {
		raw = "report"
	}

	var b strings.Builder
	for _, r := range strings.ToLower(raw) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_' || r == '.':
			b.WriteRune('_')
		}
	}

	slug := strings.Trim(b.String(), "_")
	if len(slug) > 120 {
		slug = strings.Trim(slug[:120], "_")
	}
	if slug == "" {
		slug = "report"
	}

	return slug
}

// addFileToZip copies one host file into the archive under name.
func addFileToZip(zw *zip.Writer, name, src string) error {
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", src)
	}

	dst, err := zw.Create(name)
	if err != nil {
		return err
	}

	_, err = io.Copy(dst, file)

	return err
}

// addTarToZip streams a container copy tar into the archive, mapping the copied
// path onto annexes/files/<container path relative to />.
func addTarToZip(zw *zip.Writer, reader io.Reader, containerPath, prefix string) (int, error) {
	tr := tar.NewReader(reader)
	base := path.Base(containerPath)
	rel := strings.TrimPrefix(containerPath, "/")
	count := 0

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return count, err
		}
		if header.FileInfo().IsDir() {
			continue
		}

		var target string
		switch {
		case header.Name == base:
			target = path.Join(prefix, rel)
		case strings.HasPrefix(header.Name, base+"/"):
			target = path.Join(prefix, rel, strings.TrimPrefix(header.Name, base+"/"))
		default:
			continue
		}

		dst, err := zw.Create(target)
		if err != nil {
			return count, err
		}
		if _, err := io.Copy(dst, tr); err != nil {
			return count, err
		}
		count++
	}

	return count, nil
}

// parentRef reports whether ref is a directory whose contents another ref
// already carries, so the archive lists those files once.
func parentRef(ref report.EvidenceRef, refs []report.EvidenceRef) bool {
	if ref.Kind == report.EvidenceKindScreenshot || ref.Path == "" {
		return false
	}
	prefix := strings.TrimSuffix(ref.Path, "/") + "/"
	for _, other := range refs {
		if other.Kind == report.EvidenceKindScreenshot || other.Path == ref.Path {
			continue
		}
		if strings.HasPrefix(other.Path, prefix) {
			return true
		}
	}
	return false
}
