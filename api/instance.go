package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/netlify/gotell/conf"
	"github.com/netlify/gotell/models"
	"github.com/pborman/uuid"
)

type contextKey string

const instanceContextKey = contextKey("instance")

func withInstance(ctx context.Context, instance *models.Instance) context.Context {
	return context.WithValue(ctx, instanceContextKey, instance)
}

func getInstance(ctx context.Context) *models.Instance {
	if i, ok := ctx.Value(instanceContextKey).(*models.Instance); ok {
		return i
	}
	return nil
}

func (s *Server) loadInstance(w http.ResponseWriter, r *http.Request) (context.Context, error) {
	instanceID := chi.URLParam(r, "instance_id")

	i, err := models.GetInstance(s.db, instanceID)
	if err != nil {
		jsonError(w, "Instance not found or error loading instance", http.StatusNotFound)
		return nil, err
	}
	return withInstance(r.Context(), i), nil
}

type InstanceRequestParams struct {
	UUID       string              `json:"uuid"`
	BaseConfig *conf.Configuration `json:"config"`
}

type InstanceResponse struct {
	models.Instance
	Endpoint string `json:"endpoint"`
	State    string `json:"state"`
}

func (s *Server) CreateInstance(w http.ResponseWriter, r *http.Request) {
	params := InstanceRequestParams{}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		jsonError(w, "Error decoding params", http.StatusBadRequest)
		return
	}

	_, err := models.GetInstanceByUUID(s.db, params.UUID)
	if err == nil {
		jsonError(w, "An instance with that UUID already exists", http.StatusBadRequest)
		return
	}

	i := models.Instance{
		ID:         uuid.NewRandom().String(),
		UUID:       params.UUID,
		BaseConfig: params.BaseConfig,
	}
	if err = models.CreateInstance(s.db, &i); err != nil {
		jsonError(w, "Database error creating instance", http.StatusInternalServerError)
		return
	}

	resp := InstanceResponse{
		Instance: i,
		Endpoint: "gotell API", // Can be read from config
		State:    "active",
	}
	sendJSON(w, http.StatusCreated, resp)
}

func (s *Server) GetInstance(w http.ResponseWriter, r *http.Request) {
	i := getInstance(r.Context())
	if i == nil {
		jsonError(w, "Instance not loaded", http.StatusInternalServerError)
		return
	}
	sendJSON(w, http.StatusOK, i)
}

func (s *Server) UpdateInstance(w http.ResponseWriter, r *http.Request) {
	i := getInstance(r.Context())
	if i == nil {
		jsonError(w, "Instance not loaded", http.StatusInternalServerError)
		return
	}

	params := InstanceRequestParams{}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		jsonError(w, "Error decoding params", http.StatusBadRequest)
		return
	}

	if params.BaseConfig != nil {
		i.BaseConfig = params.BaseConfig
	}

	if err := models.UpdateInstance(s.db, i); err != nil {
		jsonError(w, "Database error updating instance", http.StatusInternalServerError)
		return
	}
	sendJSON(w, http.StatusOK, i)
}

func (s *Server) DeleteInstance(w http.ResponseWriter, r *http.Request) {
	i := getInstance(r.Context())
	if i == nil {
		jsonError(w, "Instance not loaded", http.StatusInternalServerError)
		return
	}

	if err := models.DeleteInstance(s.db, i); err != nil {
		jsonError(w, "Database error deleting instance", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) instanceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, err := s.loadInstance(w, r)
		if err != nil {
			return // error already handled
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
