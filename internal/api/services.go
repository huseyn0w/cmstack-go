package api

import (
	"errors"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
	"github.com/huseyn0w/agentic-cms-go/internal/content/services"
)

// listServices serves GET /api/v1/services: a filtered, paginated listing.
func (h *handler) listServices(w http.ResponseWriter, r *http.Request) {
	page, perPage := paginate(r)
	f := services.ListFilter{
		Limit:  perPage,
		Offset: (page - 1) * perPage,
	}
	if s, ok := statusParam(r); ok {
		f.Status = &s
	}

	items, total, err := h.services.AdminList(r.Context(), f)
	if err != nil {
		Fail(w, http.StatusInternalServerError, "internal", "failed to list services")
		return
	}
	dtos := make([]serviceDTO, 0, len(items))
	for _, s := range items {
		dtos = append(dtos, toServiceDTO(s))
	}
	OK(w, http.StatusOK, listResponse{Items: dtos, Total: total, Page: page, PerPage: perPage})
}

// getService serves GET /api/v1/services/{id}: a single service with body+FAQs.
func (h *handler) getService(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "service")
	if !ok {
		return
	}
	svc, err := h.services.Get(r.Context(), actorID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	OK(w, http.StatusOK, toServiceDetailDTO(svc))
}

// serviceFAQRequest is one FAQ row in a create/patch service body.
type serviceFAQRequest struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// createServiceRequest is the JSON body for POST /api/v1/services.
type createServiceRequest struct {
	Title           string              `json:"title"`
	Slug            string              `json:"slug"`
	Summary         string              `json:"summary"`
	Body            string              `json:"body"`
	Price           string              `json:"price"`
	AreaServed      string              `json:"areaServed"`
	Status          string              `json:"status"`
	FAQs            []serviceFAQRequest `json:"faqs"`
	MetaTitle       string              `json:"metaTitle"`
	MetaDescription string              `json:"metaDescription"`
	CanonicalURL    string              `json:"canonicalUrl"`
	NoIndex         bool                `json:"noIndex"`
}

// createService serves POST /api/v1/services.
func (h *handler) createService(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req createServiceRequest
	if err := DecodeJSON(r, &req); err != nil {
		failBadJSON(w, err)
		return
	}
	in := services.CreateInput{
		Title:           req.Title,
		Slug:            req.Slug,
		Summary:         req.Summary,
		Body:            req.Body,
		Price:           req.Price,
		AreaServed:      req.AreaServed,
		Status:          kernel.ParseStatus(req.Status),
		FAQs:            faqInputs(req.FAQs),
		MetaTitle:       req.MetaTitle,
		MetaDescription: req.MetaDescription,
		CanonicalURL:    req.CanonicalURL,
		NoIndex:         req.NoIndex,
	}
	svc, err := h.services.Create(r.Context(), actorID, in)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	OK(w, http.StatusCreated, toServiceDetailDTO(svc))
}

// updateServiceRequest is the JSON body for PATCH /api/v1/services/{id}. Every
// field is a pointer so an omitted field leaves the stored value unchanged. FAQs
// are only touched when the body includes them.
type updateServiceRequest struct {
	Title           *string              `json:"title"`
	Slug            *string              `json:"slug"`
	Summary         *string              `json:"summary"`
	Body            *string              `json:"body"`
	Price           *string              `json:"price"`
	AreaServed      *string              `json:"areaServed"`
	Status          *string              `json:"status"`
	FAQs            *[]serviceFAQRequest `json:"faqs"`
	MetaTitle       *string              `json:"metaTitle"`
	MetaDescription *string              `json:"metaDescription"`
	CanonicalURL    *string              `json:"canonicalUrl"`
	NoIndex         *bool                `json:"noIndex"`
}

// updateService serves PATCH /api/v1/services/{id}.
func (h *handler) updateService(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "service")
	if !ok {
		return
	}
	var req updateServiceRequest
	if err := DecodeJSON(r, &req); err != nil {
		failBadJSON(w, err)
		return
	}
	in := services.UpdateInput{
		Title:           req.Title,
		Slug:            req.Slug,
		Summary:         req.Summary,
		Body:            req.Body,
		Price:           req.Price,
		AreaServed:      req.AreaServed,
		MetaTitle:       req.MetaTitle,
		MetaDescription: req.MetaDescription,
		CanonicalURL:    req.CanonicalURL,
		NoIndex:         req.NoIndex,
	}
	if req.Status != nil {
		s := kernel.ParseStatus(*req.Status)
		in.Status = &s
	}
	// FAQs are replaced only when the body includes them (SetFAQs stays false
	// otherwise, so an unrelated update never wipes the FAQ list).
	if req.FAQs != nil {
		in.SetFAQs = true
		in.FAQs = faqInputs(*req.FAQs)
	}

	svc, err := h.services.Update(r.Context(), actorID, id, in)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	OK(w, http.StatusOK, toServiceDetailDTO(svc))
}

// trashService serves DELETE /api/v1/services/{id}: a SOFT delete (trash).
func (h *handler) trashService(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "service")
	if !ok {
		return
	}
	if err := h.services.Trash(r.Context(), actorID, id); err != nil {
		writeServiceError(w, err)
		return
	}
	OK(w, http.StatusOK, map[string]any{"id": id.String(), "trashed": true})
}

// --- FAQ endpoints (service-scoped) -----------------------------------------

// listServiceFAQs serves GET /api/v1/services/{id}/faqs.
func (h *handler) listServiceFAQs(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "service")
	if !ok {
		return
	}
	svc, err := h.services.Get(r.Context(), actorID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	OK(w, http.StatusOK, toFaqDTOs(orderedFAQs(svc)))
}

// createServiceFAQ serves POST /api/v1/services/{id}/faqs: appends a new Q&A and
// rewrites the FULL rebuilt list (preserving order), returning the created FAQ.
func (h *handler) createServiceFAQ(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "service")
	if !ok {
		return
	}
	var req serviceFAQRequest
	if err := DecodeJSON(r, &req); err != nil {
		failBadJSON(w, err)
		return
	}

	svc, err := h.services.Get(r.Context(), actorID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	existing := orderedFAQs(svc)
	inputs := faqsToInputs(existing)
	inputs = append(inputs, services.FAQInput{Question: req.Question, Answer: req.Answer})

	reloaded, err := h.rewriteFAQs(r, actorID, id, inputs)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	// The created FAQ is the last one in the rebuilt, position-ordered list.
	created := orderedFAQs(reloaded)
	OK(w, http.StatusCreated, toFaqDTO(created[len(created)-1]))
}

// updateServiceFAQ serves PATCH /api/v1/services/{id}/faqs/{faqId}: modifies the
// matching FAQ, rewriting the full list. 404 when faqId is not on the service.
func (h *handler) updateServiceFAQ(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "service")
	if !ok {
		return
	}
	faqID, ok := faqIDParam(w, r)
	if !ok {
		return
	}
	var req serviceFAQRequest
	if err := DecodeJSON(r, &req); err != nil {
		failBadJSON(w, err)
		return
	}

	svc, err := h.services.Get(r.Context(), actorID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	existing := orderedFAQs(svc)
	idx := indexOfFAQ(existing, faqID)
	if idx < 0 {
		Fail(w, http.StatusNotFound, "not_found", "faq not found")
		return
	}
	inputs := faqsToInputs(existing)
	inputs[idx] = services.FAQInput{Question: req.Question, Answer: req.Answer}

	reloaded, err := h.rewriteFAQs(r, actorID, id, inputs)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	OK(w, http.StatusOK, toFaqDTO(orderedFAQs(reloaded)[idx]))
}

// deleteServiceFAQ serves DELETE /api/v1/services/{id}/faqs/{faqId}: removes the
// matching FAQ, rewriting the full list. 404 when faqId is absent.
func (h *handler) deleteServiceFAQ(w http.ResponseWriter, r *http.Request) {
	actorID, ok := actor(r)
	if !ok {
		Fail(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, ok := idParam(w, r, "service")
	if !ok {
		return
	}
	faqID, ok := faqIDParam(w, r)
	if !ok {
		return
	}

	svc, err := h.services.Get(r.Context(), actorID, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	existing := orderedFAQs(svc)
	idx := indexOfFAQ(existing, faqID)
	if idx < 0 {
		Fail(w, http.StatusNotFound, "not_found", "faq not found")
		return
	}
	inputs := faqsToInputs(existing)
	inputs = append(inputs[:idx], inputs[idx+1:]...)

	if _, err := h.rewriteFAQs(r, actorID, id, inputs); err != nil {
		writeServiceError(w, err)
		return
	}
	OK(w, http.StatusOK, map[string]any{"id": faqID.String(), "deleted": true})
}

// rewriteFAQs persists the FULL rebuilt FAQ list (SetFAQs=true) and returns the
// reloaded service so the caller can read back the new ids/positions.
func (h *handler) rewriteFAQs(r *http.Request, actorID, id uuid.UUID, inputs []services.FAQInput) (services.Service, error) {
	if _, err := h.services.Update(r.Context(), actorID, id, services.UpdateInput{
		SetFAQs: true,
		FAQs:    inputs,
	}); err != nil {
		return services.Service{}, err
	}
	return h.services.Get(r.Context(), actorID, id)
}

// orderedFAQs returns a service's FAQs sorted by Position (the canonical order),
// so a rebuild never depends on the repo's incidental ordering.
func orderedFAQs(svc services.Service) []services.FAQ {
	out := make([]services.FAQ, len(svc.FAQs))
	copy(out, svc.FAQs)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out
}

// faqsToInputs maps ordered domain FAQs onto FAQInputs, preserving order so no
// FAQ is lost when a single entry is mutated and the list is rewritten.
func faqsToInputs(faqs []services.FAQ) []services.FAQInput {
	out := make([]services.FAQInput, 0, len(faqs))
	for _, f := range faqs {
		out = append(out, services.FAQInput{Question: f.Question, Answer: f.Answer})
	}
	return out
}

// faqInputs maps the request FAQ rows onto FAQInputs.
func faqInputs(rows []serviceFAQRequest) []services.FAQInput {
	out := make([]services.FAQInput, 0, len(rows))
	for _, f := range rows {
		out = append(out, services.FAQInput{Question: f.Question, Answer: f.Answer})
	}
	return out
}

// indexOfFAQ returns the position (in the ordered slice) of the FAQ with id, or
// -1 when it is absent.
func indexOfFAQ(faqs []services.FAQ, id uuid.UUID) int {
	for i, f := range faqs {
		if f.ID == id {
			return i
		}
	}
	return -1
}

// faqIDParam parses the {faqId} URL parameter as a UUID, writing a 400 and
// returning ok=false on failure.
func faqIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "faqId"))
	if err != nil {
		Fail(w, http.StatusBadRequest, "invalid_id", "the faq id is not a valid uuid")
		return uuid.Nil, false
	}
	return id, true
}

// writeServiceError maps a service error onto the uniform JSON envelope:
// not-found/forbidden -> 404, title-required -> 422, everything else -> 500.
func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrNotFound), errors.Is(err, services.ErrForbidden):
		Fail(w, http.StatusNotFound, "not_found", "service not found")
	case errors.Is(err, services.ErrTitleRequired):
		FailValidation(w, map[string]string{"title": "title is required"})
	default:
		Fail(w, http.StatusInternalServerError, "internal", "failed to process service")
	}
}
