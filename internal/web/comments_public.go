package web

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/content/comments"
	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// CommentsPublicService is the subset of *comments.Service the public handler
// calls. Declaring it here keeps the handler testable with a stub.
type CommentsPublicService interface {
	PublicThread(ctx context.Context, slug string, viewer *comments.Viewer) ([]comments.PublicComment, int, error)
	Submit(ctx context.Context, in comments.SubmitInput) (comments.Comment, error)
	SelfEdit(ctx context.Context, viewer comments.Viewer, id uuid.UUID, body string) (comments.Comment, error)
	SelfDelete(ctx context.Context, viewer comments.Viewer, id uuid.UUID) error
}

// CommentsPublicHandler is the thin HTTP boundary for the public comments
// partial: it decodes, resolves the viewer/IP, calls the service, and renders the
// thread fragment. It holds NO business logic and NEVER emits author email/IP.
type CommentsPublicHandler struct {
	svc          CommentsPublicService
	csrf         func(*http.Request) string
	recaptchaKey string
}

// NewCommentsPublicHandler constructs the public comments handler.
func NewCommentsPublicHandler(svc CommentsPublicService, csrf func(*http.Request) string, recaptchaKey string) *CommentsPublicHandler {
	return &CommentsPublicHandler{svc: svc, csrf: csrf, recaptchaKey: recaptchaKey}
}

// Thread renders the comments partial for GET /blog/{slug}/comments.
func (h *CommentsPublicHandler) Thread(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	viewer := h.viewer(r)
	nodes, total, err := h.svc.PublicThread(r.Context(), slug, viewer)
	if errors.Is(err, comments.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, webtempl.CommentsSection(h.view(r, slug, nodes, total, viewer)))
}

// Submit handles POST /blog/{slug}/comments. On success it re-renders the thread
// with a "pending moderation" banner; validation/spam/rate-limit failures
// re-render the form with the mapped status and a friendly message.
func (h *CommentsPublicHandler) Submit(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	viewer := h.viewer(r)

	var parentID *uuid.UUID
	if raw := strings.TrimSpace(r.PostFormValue("parent_id")); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			parentID = &id
		}
	}

	in := comments.SubmitInput{
		Slug:           slug,
		ParentID:       parentID,
		AuthorName:     r.PostFormValue("name"),
		AuthorEmail:    r.PostFormValue("email"),
		Body:           r.PostFormValue("body"),
		ClientIP:       clientIP(r),
		RecaptchaToken: r.PostFormValue("recaptcha_token"),
		Viewer:         viewer,
	}

	_, err := h.svc.Submit(r.Context(), in)
	if err != nil {
		h.renderSubmitError(w, r, slug, viewer, in, err)
		return
	}

	nodes, total, terr := h.svc.PublicThread(r.Context(), slug, viewer)
	if terr != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	view := h.view(r, slug, nodes, total, viewer)
	view.Submitted = true
	h.render(w, r, webtempl.CommentsSection(view))
}

// SelfEdit handles POST /blog/{slug}/comments/{id}/edit (auth-gated upstream).
func (h *CommentsPublicHandler) SelfEdit(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	viewer := h.viewer(r)
	if viewer == nil {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	_, err = h.svc.SelfEdit(r.Context(), *viewer, id, r.PostFormValue("body"))
	switch {
	case errors.Is(err, comments.ErrForbidden), errors.Is(err, comments.ErrEditWindowExpired):
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	case errors.Is(err, comments.ErrNotFound):
		http.NotFound(w, r)
		return
	case errors.Is(err, comments.ErrValidation):
		http.Error(w, "invalid comment", http.StatusUnprocessableEntity)
		return
	case err != nil:
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	h.renderThread(w, r, slug, viewer)
}

// SelfDelete handles POST /blog/{slug}/comments/{id}/delete (auth-gated).
func (h *CommentsPublicHandler) SelfDelete(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	viewer := h.viewer(r)
	if viewer == nil {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = h.svc.SelfDelete(r.Context(), *viewer, id)
	switch {
	case errors.Is(err, comments.ErrForbidden), errors.Is(err, comments.ErrEditWindowExpired):
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	case errors.Is(err, comments.ErrNotFound):
		http.NotFound(w, r)
		return
	case err != nil:
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	h.renderThread(w, r, slug, viewer)
}

// --- helpers -----------------------------------------------------------------

func (h *CommentsPublicHandler) renderThread(w http.ResponseWriter, r *http.Request, slug string, viewer *comments.Viewer) {
	nodes, total, err := h.svc.PublicThread(r.Context(), slug, viewer)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, webtempl.CommentsSection(h.view(r, slug, nodes, total, viewer)))
}

func (h *CommentsPublicHandler) renderSubmitError(w http.ResponseWriter, r *http.Request, slug string, viewer *comments.Viewer, in comments.SubmitInput, err error) {
	nodes, total, terr := h.svc.PublicThread(r.Context(), slug, viewer)
	if terr != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	view := h.view(r, slug, nodes, total, viewer)
	view.PrefillName = in.AuthorName
	view.PrefillEmail = in.AuthorEmail
	view.PrefillBody = in.Body

	status := http.StatusUnprocessableEntity
	switch {
	case errors.Is(err, comments.ErrRateLimited):
		status = http.StatusTooManyRequests
		view.Error = "You are commenting too quickly. Please wait a moment and try again."
	case errors.Is(err, comments.ErrSpam):
		view.Error = "Your comment could not be verified. Please try again."
	case errors.Is(err, comments.ErrNotFound):
		http.NotFound(w, r)
		return
	case errors.Is(err, comments.ErrValidation):
		view.FieldErrors = validationFields(err, in)
		view.Error = "Please check the form and try again."
	default:
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	if rerr := render.Component(r.Context(), w, status, webtempl.CommentsSection(view)); rerr != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// validationFields maps a validation error to per-field messages for the summary.
func validationFields(_ error, in comments.SubmitInput) map[string]string {
	fields := map[string]string{}
	if in.Viewer == nil {
		if strings.TrimSpace(in.AuthorName) == "" {
			fields["name"] = "Name is required."
		}
		if strings.TrimSpace(in.AuthorEmail) == "" {
			fields["email"] = "Email is required."
		}
	}
	if strings.TrimSpace(in.Body) == "" {
		fields["body"] = "Comment cannot be empty."
	}
	if len(fields) == 0 {
		fields["body"] = "Your comment could not be posted."
	}
	return fields
}

// view assembles the CommentThreadView from the domain thread.
func (h *CommentsPublicHandler) view(r *http.Request, slug string, nodes []comments.PublicComment, total int, viewer *comments.Viewer) webtempl.CommentThreadView {
	base := "/blog/" + slug + "/comments"
	return webtempl.CommentThreadView{
		PostSlug:     slug,
		Count:        total,
		Comments:     commentNodes(nodes, base),
		SubmitURL:    base,
		CSRFToken:    h.csrfToken(r),
		IsGuest:      viewer == nil,
		RecaptchaKey: h.recaptchaKey,
		FieldErrors:  map[string]string{},
	}
}

// commentNodes maps the domain PublicComment tree to the view CommentNode tree,
// deriving avatar initials + control URLs. It NEVER reads email/IP (the domain
// projection does not carry them).
func commentNodes(in []comments.PublicComment, base string) []webtempl.CommentNode {
	out := make([]webtempl.CommentNode, 0, len(in))
	for _, c := range in {
		node := webtempl.CommentNode{
			ID:         c.ID.String(),
			AuthorName: c.AuthorName,
			Initials:   commentInitials(c.AuthorName),
			Body:       c.Body,
			Date:       c.CreatedAt.Format("Jan 2, 2006 15:04"),
			Edited:     c.Edited,
			Mine:       c.Mine,
			Pending:    c.Pending,
			EditURL:    base + "/" + c.ID.String() + "/edit",
			DeleteURL:  base + "/" + c.ID.String() + "/delete",
			ReplyToID:  c.ID.String(),
			Replies:    commentNodes(c.Replies, base),
		}
		out = append(out, node)
	}
	return out
}

// commentInitials returns up to two uppercase initials from a display name.
func commentInitials(name string) string {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return "?"
	}
	var b strings.Builder
	for _, f := range fields {
		b.WriteString(strings.ToUpper(f[:1]))
		if b.Len() == 2 {
			break
		}
	}
	return b.String()
}

// viewer resolves the signed-in commenter, or nil for a guest.
func (h *CommentsPublicHandler) viewer(r *http.Request) *comments.Viewer {
	u, ok := UserFromContext(r.Context())
	if !ok || u.ID == uuid.Nil {
		return nil
	}
	name := u.Name
	if name == "" {
		name = u.Email
	}
	return &comments.Viewer{ID: u.ID, Name: name, Email: u.Email}
}

func (h *CommentsPublicHandler) csrfToken(r *http.Request) string {
	if h.csrf == nil {
		return ""
	}
	return h.csrf(r)
}

func (h *CommentsPublicHandler) render(w http.ResponseWriter, r *http.Request, c webtempl.Component) {
	if err := render.Component(r.Context(), w, http.StatusOK, c); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// clientIP returns the real client IP from RemoteAddr (host only). The router
// intentionally does not trust X-Forwarded-For (spoofable); RemoteAddr is the
// honest source until a trusted proxy is configured.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
