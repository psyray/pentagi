package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"pentagi/pkg/database"
	"pentagi/pkg/database/knowledge/limits"
	"pentagi/pkg/graph/model"
	obs "pentagi/pkg/observability"
	"pentagi/pkg/observability/langfuse"
	"pentagi/pkg/providers/embeddings"

	"github.com/sirupsen/logrus"
	"github.com/vxcontrol/cloud/anonymizer"
	"github.com/vxcontrol/langchaingo/documentloaders"
	"github.com/vxcontrol/langchaingo/schema"
	"github.com/vxcontrol/langchaingo/vectorstores"
	"github.com/vxcontrol/langchaingo/vectorstores/pgvector"
)

const (
	codeVectorStoreThreshold   = 0.2
	codeVectorStoreResultLimit = 3
	codeVectorStoreDefaultType = "code"
	codeVectorStoreDefaultLang = CodeLang("text")
	codeNotFoundMessage        = "nothing found in code samples store and you need to store it after figure out this case"
)

type code struct {
	userID            int64
	flowID            int64
	taskID            *int64
	subtaskID         *int64
	replacer          anonymizer.Replacer
	store             *pgvector.Store
	embedder          embeddings.Embedder
	db                database.Querier
	maxEmbeddingBytes int
	vslp              VectorStoreLogProvider
	knp               KnowledgeProvider
	sharedSearch      bool
	deduplicate       bool
	skillSearcher     SkillSearcher
}

func NewCodeTool(
	userID int64,
	flowID int64,
	taskID, subtaskID *int64,
	replacer anonymizer.Replacer,
	store *pgvector.Store,
	embedder embeddings.Embedder,
	db database.Querier,
	maxEmbeddingBytes int,
	vslp VectorStoreLogProvider,
	knp KnowledgeProvider,
	opts ...KnowledgeSearchOption,
) Tool {
	scope := resolveKnowledgeSearchOptions(opts)
	return &code{
		userID:            userID,
		flowID:            flowID,
		taskID:            taskID,
		subtaskID:         subtaskID,
		replacer:          replacer,
		store:             store,
		embedder:          embedder,
		db:                db,
		maxEmbeddingBytes: maxEmbeddingBytes,
		vslp:              vslp,
		knp:               knp,
		sharedSearch:      scope.shared,
		deduplicate:       scope.deduplicate,
		skillSearcher:     scope.skillSearcher,
	}
}

func (c *code) Handle(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if !c.IsAvailable() {
		return "", fmt.Errorf("code is not available")
	}

	ctx, observation := obs.Observer.NewObservation(ctx)
	logger := logrus.WithContext(ctx).WithFields(enrichLogrusFields(c.flowID, c.taskID, c.subtaskID, logrus.Fields{
		"tool": name,
		"args": string(args),
	}))

	if c.store == nil {
		logger.Error("pgvector store is not initialized")
		return "", fmt.Errorf("pgvector store is not initialized")
	}

	switch name {
	case SearchCodeToolName:
		var action SearchCodeAction
		if err := json.Unmarshal(args, &action); err != nil {
			logger.WithError(err).Error("failed to unmarshal search code action")
			return "", fmt.Errorf("failed to unmarshal %s search code action arguments: %w", name, err)
		}

		filters := map[string]any{
			"doc_type":  codeVectorStoreDefaultType,
			"code_lang": action.Lang,
		}
		maps.Copy(filters, knowledgeScopeFilters(c.userID, c.sharedSearch))

		metadata := langfuse.Metadata{
			"tool_name":     name,
			"code_lang":     action.Lang,
			"message":       action.Message,
			"limit":         codeVectorStoreResultLimit,
			"threshold":     codeVectorStoreThreshold,
			"doc_type":      codeVectorStoreDefaultType,
			"queries_count": len(action.Questions),
		}

		retriever := observation.Retriever(
			langfuse.WithRetrieverName("retrieve code samples from vector store"),
			langfuse.WithRetrieverInput(map[string]any{
				"queries":     action.Questions,
				"threshold":   codeVectorStoreThreshold,
				"max_results": codeVectorStoreResultLimit,
				"filters":     filters,
			}),
			langfuse.WithRetrieverMetadata(metadata),
		)
		ctx, observation = retriever.Observation(ctx)

		logger = logger.WithFields(logrus.Fields{
			"queries_count": len(action.Questions),
			"lang":          action.Lang,
			"filters":       filters,
		})

		var allDocs []schema.Document
		for i, query := range action.Questions {
			queryLogger := logger.WithFields(logrus.Fields{
				"query_index": i + 1,
				"query":       query[:min(len(query), 1000)],
			})

			docs, err := c.store.SimilaritySearch(
				ctx,
				query,
				codeVectorStoreResultLimit,
				vectorstores.WithScoreThreshold(codeVectorStoreThreshold),
				vectorstores.WithFilters(filters),
			)
			if err != nil {
				queryLogger.WithError(err).Error("failed to search code samples for query")
				continue // Continue with other queries even if one fails
			}

			queryLogger.WithField("docs_found", len(docs)).Debug("query executed")
			allDocs = append(allDocs, docs...)
		}

		logger.WithFields(logrus.Fields{
			"total_docs_before_dedup": len(allDocs),
		}).Debug("all queries completed")

		docs := MergeAndDeduplicateDocs(allDocs, codeVectorStoreResultLimit)

		logger.WithFields(logrus.Fields{
			"docs_after_dedup": len(docs),
		}).Debug("documents deduplicated and sorted")

		if len(docs) == 0 {
			retriever.End(
				langfuse.WithRetrieverStatus("no code samples found"),
				langfuse.WithRetrieverLevel(langfuse.ObservationLevelWarning),
				langfuse.WithRetrieverOutput([]any{}),
			)
			observation.Score(
				langfuse.WithScoreComment("no code samples found"),
				langfuse.WithScoreName("code_search_result"),
				langfuse.WithScoreStringValue("not_found"),
			)
			logKnowledgeOperation(ctx, c.vslp, c.replacer, c.taskID, c.subtaskID, filters,
				strings.Join(action.Questions, "\n--------------------------------\n"),
				database.VecstoreActionTypeRetrieve, codeNotFoundMessage)
			return appendSkillSuggestions(ctx, c.skillSearcher, c.userID, strings.Join(action.Questions, " "), codeNotFoundMessage), nil
		}

		retriever.End(
			langfuse.WithRetrieverStatus("success"),
			langfuse.WithRetrieverLevel(langfuse.ObservationLevelDebug),
			langfuse.WithRetrieverOutput(docs),
		)

		buffer := strings.Builder{}
		for i, doc := range docs {
			observation.Score(
				langfuse.WithScoreComment("code samples vector store result"),
				langfuse.WithScoreName("code_search_result"),
				langfuse.WithScoreFloatValue(float64(doc.Score)),
			)
			fmt.Fprintf(&buffer, "# Document %d Match score: %f\n\n", i+1, doc.Score)
			fmt.Fprintf(&buffer, "## Original Code Question\n\n%s\n\n", doc.Metadata["question"])
			fmt.Fprintf(&buffer, "## Original Code Description\n\n%s\n\n", doc.Metadata["description"])
			buffer.WriteString("## Content\n\n")
			buffer.WriteString(doc.PageContent)
			buffer.WriteString("\n\n")
		}

		result := clampKnowledgeResult(buffer.String(), DefaultResultSizeLimit)
		result = appendSkillSuggestions(ctx, c.skillSearcher, c.userID, strings.Join(action.Questions, " "), result)

		logKnowledgeOperation(ctx, c.vslp, c.replacer, c.taskID, c.subtaskID, filters,
			strings.Join(action.Questions, "\n--------------------------------\n"),
			database.VecstoreActionTypeRetrieve, result)

		return result, nil

	case StoreCodeToolName:
		var action StoreCodeAction
		if err := json.Unmarshal(args, &action); err != nil {
			logger.WithError(err).Error("failed to unmarshal store code action")
			return "", fmt.Errorf("failed to unmarshal %s store code action arguments: %w", name, err)
		}

		action.Question = limits.FillBlankQuestion(action.Question, action.Description, "Untitled code snippet")
		if strings.TrimSpace(action.Lang.String()) == "" {
			action.Lang = codeVectorStoreDefaultLang
		}

		renderedCode := c.renderCode(action.Explanation, action.Lang.String(), action.Code)

		// Anonymize before anything else so all downstream paths (including error
		// branches that emit langfuse events) only ever expose the anonymized form.
		var (
			anonymizedCode        = boundedContent(c.replacer, renderedCode)
			anonymizedQuestion    = limits.TruncateToLimit(c.replacer.ReplaceString(action.Question), limits.MaxQuestionLen)
			anonymizedDescription = limits.TruncateToLimit(c.replacer.ReplaceString(action.Description), limits.MaxDescriptionLen)
		)
		contentHash := knowledgeFingerprint(anonymizedCode)

		eventMetadata := map[string]any{
			"tool_name":   name,
			"code_lang":   action.Lang,
			"message":     action.Message,
			"description": anonymizedDescription,
			"doc_type":    codeVectorStoreDefaultType,
		}
		opts := []langfuse.EventOption{
			langfuse.WithEventName("store code samples to vector store"),
			langfuse.WithEventInput(action.Question),
			langfuse.WithEventOutput(anonymizedCode),
			langfuse.WithEventMetadata(eventMetadata),
		}

		logger = logger.WithFields(logrus.Fields{
			"query": action.Question[:min(len(action.Question), 1000)],
			"lang":  action.Lang,
			"code":  action.Code[:min(len(action.Code), 1000)],
		})

		metadata := map[string]any{
			"user_id":      c.userID,
			"flow_id":      c.flowID,
			"doc_type":     codeVectorStoreDefaultType,
			"code_lang":    action.Lang,
			"question":     anonymizedQuestion,
			"description":  anonymizedDescription,
			"content_hash": contentHash,
			"part_size":    len(anonymizedCode),
			"total_size":   len(anonymizedCode),
		}
		if c.taskID != nil {
			metadata["task_id"] = *c.taskID
		}
		if c.subtaskID != nil {
			metadata["subtask_id"] = *c.subtaskID
		}

		dedupFilters := map[string]any{
			"doc_type":  codeVectorStoreDefaultType,
			"code_lang": action.Lang,
		}
		maps.Copy(dedupFilters, knowledgeScopeFilters(c.userID, c.sharedSearch))

		if c.deduplicate {
			dup, dedupErr := findKnowledgeDuplicate(ctx, c.store, dedupFilters, anonymizedQuestion, contentHash)
			if dedupErr != nil {
				logger.WithError(dedupErr).Warn("knowledge deduplication lookup failed; storing anyway")
			}
			if dup != nil {
				message := describeKnowledgeDuplicate("code sample", dup)
				observation.Event(append(opts,
					langfuse.WithEventStatus("duplicate"),
					langfuse.WithEventLevel(langfuse.ObservationLevelDebug),
					langfuse.WithEventOutput(message),
				)...)
				logKnowledgeOperation(ctx, c.vslp, c.replacer, c.taskID, c.subtaskID, dedupFilters,
					anonymizedQuestion, database.VecstoreActionTypeStore, message)
				logger.WithFields(logrus.Fields{
					"duplicate":  true,
					"similarity": dup.Document.Score,
				}).Info("code storage skipped: duplicate")
				return message, nil
			}
		}

		var (
			docs []schema.Document
			err  error
			ids  []string
		)

		if len(anonymizedCode) <= c.maxEmbeddingBytes || c.embedder == nil {
			// Fast path: document fits within the embedding limit — use AddDocuments normally.
			docs, err = documentloaders.NewText(strings.NewReader(anonymizedCode)).Load(ctx)
			if err != nil {
				observation.Event(append(opts,
					langfuse.WithEventStatus(err.Error()),
					langfuse.WithEventLevel(langfuse.ObservationLevelError),
				)...)
				logger.WithError(err).Error("failed to load document")
				return "", fmt.Errorf("failed to load document: %w", err)
			}
			for i := range docs {
				if docs[i].Metadata == nil {
					docs[i].Metadata = map[string]any{}
				}
				maps.Copy(docs[i].Metadata, metadata)
				docs[i].Metadata["part_size"] = len(docs[i].PageContent)
			}
			ids, err = c.store.AddDocuments(ctx, docs)
			eventMetadata["ids"] = ids
			if err != nil {
				observation.Event(append(opts,
					langfuse.WithEventStatus(err.Error()),
					langfuse.WithEventLevel(langfuse.ObservationLevelError),
				)...)
				logger.WithError(err).Error("failed to store code sample")
				return "", fmt.Errorf("failed to store code sample: %w", err)
			}
		} else {
			// Slow path: document exceeds embedding limit — embed truncated text,
			// store the full anonymized document in the database.
			embeddingText := truncateForEmbedding(anonymizedCode, c.maxEmbeddingBytes)

			id, err := storeDocumentWithEmbeddingLimit(ctx, c.db, c.embedder,
				embeddingText, anonymizedCode, metadata)
			if err != nil {
				observation.Event(append(opts,
					langfuse.WithEventStatus(err.Error()),
					langfuse.WithEventLevel(langfuse.ObservationLevelError),
				)...)
				logger.WithError(err).Error("failed to store code sample with embedding limit")
				return "", fmt.Errorf("failed to store code sample: %w", err)
			}
			ids = []string{id}
			docs = []schema.Document{
				{
					PageContent: anonymizedCode,
					Metadata:    metadata,
				},
			}
			eventMetadata["ids"] = ids
		}

		observation.Event(append(opts,
			langfuse.WithEventStatus("success"),
			langfuse.WithEventLevel(langfuse.ObservationLevelDebug),
			langfuse.WithEventOutput(docs),
		)...)

		if c.knp != nil {
			codeLang := action.Lang.String()
			for _, id := range ids {
				knDoc := &model.KnowledgeDocument{
					ID:          id,
					UserID:      c.userID,
					DocType:     model.KnowledgeDocTypeCode,
					Content:     anonymizedCode,
					Question:    anonymizedQuestion,
					Description: &anonymizedDescription,
					CodeLang:    &codeLang,
					PartSize:    len(anonymizedCode),
					TotalSize:   len(anonymizedCode),
					Manual:      false,
				}
				if c.flowID != 0 {
					knDoc.FlowID = &c.flowID
				}
				knDoc.TaskID = c.taskID
				knDoc.SubtaskID = c.subtaskID
				c.knp.KnowledgeDocumentCreated(ctx, knDoc)
			}
		}

		storeFilters := map[string]any{
			"doc_type":  codeVectorStoreDefaultType,
			"code_lang": action.Lang,
		}
		if c.taskID != nil {
			storeFilters["task_id"] = *c.taskID
		}
		if c.subtaskID != nil {
			storeFilters["subtask_id"] = *c.subtaskID
		}
		logKnowledgeOperation(ctx, c.vslp, c.replacer, c.taskID, c.subtaskID, storeFilters,
			anonymizedQuestion, database.VecstoreActionTypeStore, anonymizedCode)

		return "code sample stored successfully", nil

	default:
		logger.Error("unknown tool")
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (c *code) IsAvailable() bool {
	return c.store != nil
}

func (c *code) renderCode(explanation, lang, code string) string {
	buffer := strings.Builder{}
	buffer.WriteString(explanation)
	buffer.WriteString("\n\n")
	buffer.WriteString("```")
	buffer.WriteString(lang)
	buffer.WriteString("\n")
	buffer.WriteString(code)
	buffer.WriteString("\n```")
	return buffer.String()
}
