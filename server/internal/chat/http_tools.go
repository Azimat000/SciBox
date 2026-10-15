package chat

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scibox/server/internal/apierr"
)

// Адреса инструментов переписки (срез 38): блокировка, жалобы, шаблоны ответов.

func (h *Handler) block(w http.ResponseWriter, r *http.Request) {
	id, ok := chatID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	b, err := h.svc.Block(r.Context(), actor(r), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusCreated, map[string]Block{"block": b})
}

func (h *Handler) blocks(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.Blocks(r.Context(), actor(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) unblock(w http.ResponseWriter, r *http.Request) {
	org, err := uuid.Parse(chi.URLParam(r, "orgId"))
	if err != nil {
		h.notFound(w, r)
		return
	}
	if err := h.svc.Unblock(r.Context(), actor(r), org); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) complain(w http.ResponseWriter, r *http.Request) {
	id, ok := chatID(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	var in ComplaintInput
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	c, err := h.svc.Complain(r.Context(), actor(r), id, in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusCreated, map[string]Complaint{"complaint": c})
}

// orgParam разбирает номер организации из адреса; негодный номер даёт тот же 404, что и чужая организация.
func orgParam(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "orgId"))
	return id, err == nil
}

func (h *Handler) templates(w http.ResponseWriter, r *http.Request) {
	org, ok := orgParam(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	list, err := h.svc.Templates(r.Context(), actor(r), org)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, list)
}

func (h *Handler) createTemplate(w http.ResponseWriter, r *http.Request) {
	org, ok := orgParam(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	var in TemplateInput
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	t, err := h.svc.CreateTemplate(r.Context(), actor(r), org, in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusCreated, map[string]Template{"template": t})
}

// templateTarget разбирает организацию и шаблон из адреса. Негодный номер шаблона отвечает после проверки прав на
// организацию: чужая организация даёт свой 404, а не «нет такого шаблона».
func (h *Handler) templateTarget(w http.ResponseWriter, r *http.Request) (org, tid uuid.UUID, ok bool) {
	org, ok = orgParam(r)
	if !ok {
		h.notFound(w, r)
		return org, tid, false
	}
	tid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		if _, e := h.svc.Templates(r.Context(), actor(r), org); e != nil {
			h.fail(w, r, e)
		} else {
			h.fail(w, r, ErrTemplateNotFound)
		}
		return org, tid, false
	}
	return org, tid, true
}

func (h *Handler) updateTemplate(w http.ResponseWriter, r *http.Request) {
	org, tid, ok := h.templateTarget(w, r)
	if !ok {
		return
	}
	var in TemplateInput
	if !apierr.DecodeJSON(w, r, &in, maxBody) {
		return
	}
	t, err := h.svc.UpdateTemplate(r.Context(), actor(r), org, tid, in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	apierr.WriteJSON(w, http.StatusOK, map[string]Template{"template": t})
}

func (h *Handler) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	org, tid, ok := h.templateTarget(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteTemplate(r.Context(), actor(r), org, tid); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
