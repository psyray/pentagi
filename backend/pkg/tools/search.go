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
	searchVectorStoreThreshold   = 0.2
	searchVectorStoreResultLimit = 3
	searchVectorStoreDefaultType = "answer"
	searchNotFoundMessage        = "nothing found in answer store and you need to store it after figure out this case"
)

type search struct {
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
}

func NewSearchTool(
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
	return &search{
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
	}
}

func (s *search) Handle(ctx context.Context, name string, args json.RawMessage) (string, error) {
	ctx, observation := obs.Observer.NewObservation(ctx)
	logger := logrus.WithContext(ctx).WithFields(enrichLogrusFields(s.flowID, s.taskID, s.subtaskID, logrus.Fields{
		"tool": name,
		"args": string(args),
	}))

	if s.store == nil {
		logger.Error("pgvector store is not initialized")
		return "", fmt.Errorf("pgvector store is not initialized")
	}

	switch name {
	case SearchAnswerToolName:
		var action SearchAnswerAction
		if err := json.Unmarshal(args, &action); err != nil {
			logger.WithError(err).Error("failed to unmarshal search answer action arguments")
			return "", fmt.Errorf("failed to unmarshal %s search answer action arguments: %w", name, err)
		}

		filters := map[string]any{
			"doc_type":    searchVectorStoreDefaultType,
			"answer_type": action.Type,
		}
		maps.Copy(filters, knowledgeScopeFilters(s.userID, s.sharedSearch))

		metadata := langfuse.Metadata{
			"tool_name":     name,
			"message":       action.Message,
			"limit":         searchVectorStoreResultLimit,
			"threshold":     searchVectorStoreThreshold,
			"doc_type":      searchVectorStoreDefaultType,
			"answer_type":   action.Type,
			"queries_count": len(action.Questions),
		}

		retriever := observation.Retriever(
			langfuse.WithRetrieverName("retrieve search answer from vector store"),
			langfuse.WithRetrieverInput(map[string]any{
				"queries":     action.Questions,
				"threshold":   searchVectorStoreThreshold,
				"max_results": searchVectorStoreResultLimit,
				"filters":     filters,
			}),
			langfuse.WithRetrieverMetadata(metadata),
		)
		ctx, observation = retriever.Observation(ctx)

		logger = logger.WithFields(logrus.Fields{
			"queries_count": len(action.Questions),
			"answer_type":   action.Type,
		})

		var allDocs []schema.Document
		for i, query := range action.Questions {
			queryLogger := logger.WithFields(logrus.Fields{
				"query_index": i + 1,
				"query":       query[:min(len(query), 1000)],
			})

			docs, err := s.store.SimilaritySearch(
				ctx,
				query,
				searchVectorStoreResultLimit,
				vectorstores.WithScoreThreshold(searchVectorStoreThreshold),
				vectorstores.WithFilters(filters),
			)
			if err != nil {
				obs.LogErrorOrCancel(queryLogger, err, "failed to search answer for query")
				continue // Continue with other queries even if one fails
			}

			queryLogger.WithField("docs_found", len(docs)).Debug("query executed")
			allDocs = append(allDocs, docs...)
		}

		logger.WithFields(logrus.Fields{
			"total_docs_before_dedup": len(allDocs),
		}).Debug("all queries completed")

		docs := MergeAndDeduplicateDocs(allDocs, searchVectorStoreResultLimit)

		logger.WithFields(logrus.Fields{
			"docs_after_dedup": len(docs),
		}).Debug("documents deduplicated and sorted")

		if len(docs) == 0 {
			retriever.End(
				langfuse.WithRetrieverStatus("no search answer found"),
				langfuse.WithRetrieverLevel(langfuse.ObservationLevelWarning),
				langfuse.WithRetrieverOutput([]any{}),
			)
			observation.Score(
				langfuse.WithScoreComment("no search answer found"),
				langfuse.WithScoreName("search_answer_result"),
				langfuse.WithScoreStringValue("not_found"),
			)
			logKnowledgeOperation(ctx, s.vslp, s.replacer, s.taskID, s.subtaskID, filters,
				strings.Join(action.Questions, "\n--------------------------------\n"),
				database.VecstoreActionTypeRetrieve, searchNotFoundMessage)
			return searchNotFoundMessage, nil
		}

		retriever.End(
			langfuse.WithRetrieverStatus("success"),
			langfuse.WithRetrieverLevel(langfuse.ObservationLevelDebug),
			langfuse.WithRetrieverOutput(docs),
		)

		buffer := strings.Builder{}
		for i, doc := range docs {
			observation.Score(
				langfuse.WithScoreComment("search answer vector store result"),
				langfuse.WithScoreName("search_answer_result"),
				langfuse.WithScoreFloatValue(float64(doc.Score)),
			)
			fmt.Fprintf(&buffer, "# Document %d Search Score: %f\n\n", i+1, doc.Score)
			fmt.Fprintf(&buffer, "## Original Answer Type: %s\n\n", doc.Metadata["answer_type"])
			fmt.Fprintf(&buffer, "## Original Search Question\n\n%s\n\n", doc.Metadata["question"])
			buffer.WriteString("## Content\n\n")
			buffer.WriteString(doc.PageContent)
			buffer.WriteString("\n\n")
		}

		result := clampKnowledgeResult(buffer.String(), DefaultResultSizeLimit)

		logKnowledgeOperation(ctx, s.vslp, s.replacer, s.taskID, s.subtaskID, filters,
			strings.Join(action.Questions, "\n--------------------------------\n"),
			database.VecstoreActionTypeRetrieve, result)

		return result, nil

	case StoreAnswerToolName:
		var action StoreAnswerAction
		if err := json.Unmarshal(args, &action); err != nil {
			logger.WithError(err).Error("failed to unmarshal search answer action arguments")
			return "", fmt.Errorf("failed to unmarshal %s store answer action arguments: %w", name, err)
		}

		action.Question = limits.FillBlankQuestion(action.Question, action.Answer, "Untitled answer")
		if strings.TrimSpace(string(action.Type)) == "" {
			action.Type = AnswerType(model.KnowledgeAnswerTypeOther)
		}

		// Anonymize before anything else so all downstream paths (including error
		// branches that emit langfuse events) only ever expose the anonymized form.
		var (
			anonymizedAnswer   = boundedContent(s.replacer, action.Answer)
			anonymizedQuestion = limits.TruncateToLimit(s.replacer.ReplaceString(action.Question), limits.MaxQuestionLen)
		)
		contentHash := knowledgeFingerprint(anonymizedAnswer)

		eventMetadata := map[string]any{
			"tool_name":   name,
			"message":     action.Message,
			"doc_type":    searchVectorStoreDefaultType,
			"answer_type": action.Type,
		}
		opts := []langfuse.EventOption{
			langfuse.WithEventName("store search answer to vector store"),
			langfuse.WithEventInput(action.Question),
			langfuse.WithEventOutput(anonymizedAnswer),
			langfuse.WithEventMetadata(eventMetadata),
		}

		logger = logger.WithFields(logrus.Fields{
			"query":       action.Question[:min(len(action.Question), 1000)],
			"answer_type": action.Type,
			"answer":      action.Answer[:min(len(action.Answer), 1000)],
		})

		metadata := map[string]any{
			"user_id":      s.userID,
			"flow_id":      s.flowID,
			"doc_type":     searchVectorStoreDefaultType,
			"answer_type":  action.Type,
			"question":     anonymizedQuestion,
			"content_hash": contentHash,
			"part_size":    len(anonymizedAnswer),
			"total_size":   len(anonymizedAnswer),
		}
		if s.taskID != nil {
			metadata["task_id"] = *s.taskID
		}
		if s.subtaskID != nil {
			metadata["subtask_id"] = *s.subtaskID
		}

		dedupFilters := map[string]any{
			"doc_type":    searchVectorStoreDefaultType,
			"answer_type": action.Type,
		}
		maps.Copy(dedupFilters, knowledgeScopeFilters(s.userID, s.sharedSearch))

		if s.deduplicate {
			dup, dedupErr := findKnowledgeDuplicate(ctx, s.store, dedupFilters, anonymizedQuestion, contentHash)
			if dedupErr != nil {
				logger.WithError(dedupErr).Warn("knowledge deduplication lookup failed; storing anyway")
			}
			if dup != nil {
				message := describeKnowledgeDuplicate("answer", dup)
				observation.Event(append(opts,
					langfuse.WithEventStatus("duplicate"),
					langfuse.WithEventLevel(langfuse.ObservationLevelDebug),
					langfuse.WithEventOutput(message),
				)...)
				logKnowledgeOperation(ctx, s.vslp, s.replacer, s.taskID, s.subtaskID, dedupFilters,
					anonymizedQuestion, database.VecstoreActionTypeStore, message)
				logger.WithFields(logrus.Fields{
					"duplicate":  true,
					"similarity": dup.Document.Score,
				}).Info("answer storage skipped: duplicate")
				return message, nil
			}
		}

		var (
			docs []schema.Document
			ids  []string
			err  error
		)

		if len(anonymizedAnswer) <= s.maxEmbeddingBytes || s.embedder == nil {
			// Fast path: answer fits within the embedding limit.
			docs, err = documentloaders.NewText(strings.NewReader(anonymizedAnswer)).Load(ctx)
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
			ids, err = s.store.AddDocuments(ctx, docs)
			eventMetadata["ids"] = ids
			if err != nil {
				observation.Event(append(opts,
					langfuse.WithEventStatus(err.Error()),
					langfuse.WithEventLevel(langfuse.ObservationLevelError),
				)...)
				logger.WithError(err).Error("failed to store answer for question")
				return "", fmt.Errorf("failed to store answer for question: %w", err)
			}
		} else {
			// Slow path: Answer field exceeds embedding limit.
			// PageContent is just the answer text, so overhead = 0.
			embeddingText := truncateForEmbedding(anonymizedAnswer, s.maxEmbeddingBytes)

			id, err := storeDocumentWithEmbeddingLimit(ctx, s.db, s.embedder,
				embeddingText, anonymizedAnswer, metadata)
			if err != nil {
				observation.Event(append(opts,
					langfuse.WithEventStatus(err.Error()),
					langfuse.WithEventLevel(langfuse.ObservationLevelError),
				)...)
				logger.WithError(err).Error("failed to store answer with embedding limit")
				return "", fmt.Errorf("failed to store answer for question: %w", err)
			}
			ids = []string{id}
			docs = []schema.Document{
				{
					PageContent: anonymizedAnswer,
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

		if s.knp != nil {
			answerType := model.KnowledgeAnswerType(action.Type)
			for _, id := range ids {
				knDoc := &model.KnowledgeDocument{
					ID:         id,
					UserID:     s.userID,
					DocType:    model.KnowledgeDocTypeAnswer,
					Content:    anonymizedAnswer,
					Question:   anonymizedQuestion,
					AnswerType: &answerType,
					PartSize:   len(anonymizedAnswer),
					TotalSize:  len(anonymizedAnswer),
					Manual:     false,
				}
				if s.flowID != 0 {
					knDoc.FlowID = &s.flowID
				}
				knDoc.TaskID = s.taskID
				knDoc.SubtaskID = s.subtaskID
				s.knp.KnowledgeDocumentCreated(ctx, knDoc)
			}
		}

		storeFilters := map[string]any{
			"doc_type":    searchVectorStoreDefaultType,
			"answer_type": action.Type,
		}
		if s.taskID != nil {
			storeFilters["task_id"] = *s.taskID
		}
		if s.subtaskID != nil {
			storeFilters["subtask_id"] = *s.subtaskID
		}
		logKnowledgeOperation(ctx, s.vslp, s.replacer, s.taskID, s.subtaskID, storeFilters,
			anonymizedQuestion, database.VecstoreActionTypeStore, anonymizedAnswer)

		return "answer for question stored successfully", nil

	default:
		logger.Error("unknown tool")
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (s *search) IsAvailable() bool {
	return s.store != nil
}
