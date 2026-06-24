package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jinzhu/gorm"
	"github.com/sirupsen/logrus"
	jwt "github.com/dgrijalva/jwt-go"
	"code.gitea.io/sdk/gitea"
	"github.com/netlify/gotell/comments"
	"github.com/netlify/gotell/conf"
	"github.com/rs/cors"
	"github.com/zenazn/goji/web/mutil"
)

const defaultVersion = "unknown version"

var threadRegexp = regexp.MustCompile(`(\d+)-(\d+)-(.+)`)
var slugify = regexp.MustCompile(`\W`)
var squeeze = regexp.MustCompile(`-+`)
var bearerRegexp = regexp.MustCompile(`^(?:B|b)earer (\S+$)`)

type Server struct {
	handler  http.Handler
	config   *conf.Configuration
	client   *gitea.Client
	db       *gorm.DB
	settings *settings
	mutex    sync.Mutex
	version  string
}

func Min(x, y int) int {
	if x < y {
		return x
	}
	return y
}

func (s *Server) postComment(w http.ResponseWriter, req *http.Request) {
	entryPath := req.URL.Path
	if chi.RouteContext(req.Context()) != nil && chi.RouteContext(req.Context()).RoutePattern() != "" {
		// When mounted under /instances/{instance_id}, chi doesn't strip the prefix automatically in URL.Path
		// The actual comment path parameter from chi might be "*"
		// Let's extract the '*' path param which represents the original requested path after instances/{instance_id}/
		paramPath := chi.URLParam(req, "*")
		if paramPath != "" {
			entryPath = "/" + paramPath
		}
	}

	w.Header().Set("Content-Type", "application/json")

	// Get instance client and configuration
	client := s.client
	config := s.config
	instance := getInstance(req.Context())
	if instance != nil {
		instanceConfig, err := instance.Config()
		if err == nil && instanceConfig != nil {
			config = instanceConfig
			forgejoURL := config.API.ForgejoURL
			if forgejoURL == "" {
				forgejoURL = "https://v15.next.forgejo.org"
			}
			newClient, err := gitea.NewClient(forgejoURL, gitea.SetToken(config.API.AccessToken))
			if err == nil {
				client = newClient
			}
		}
	}

	settings := s.getSettings() // We might want to pass config here too if it uses config.API.SiteURL.
	// Actually s.getSettings uses s.config.API.SiteURL, let's just make it use the local config
	if instance != nil {
		resp, err := http.Get(config.API.SiteURL + "/gotell/settings.json")
		if err == nil {
			defer resp.Body.Close()
			type settingsStruct struct {
				BannedIPs       []string `json:"banned_ips"`
				BannedKeywords  []string `json:"banned_keywords"`
				BannedEmails    []string `json:"banned_emails"`
				RequireApproval bool     `json:"require_approval"`
				TimeLimit       int      `json:"timelimit"`
			}
			st := &settingsStruct{}
			if json.NewDecoder(resp.Body).Decode(st) == nil {
				settings.BannedIPs = st.BannedIPs
				settings.BannedKeywords = st.BannedKeywords
				settings.BannedEmails = st.BannedEmails
				settings.RequireApproval = st.RequireApproval
				settings.TimeLimit = st.TimeLimit
			}
		}
	}

	for _, ip := range settings.BannedIPs {
		if req.RemoteAddr == ip {
			w.Header().Add("X-Banned", "IP-Banned")
			fmt.Fprintln(w, "{}")
			return
		}
	}

	entryData, err := s.entryDataConfig(entryPath, config)
	if err != nil {
		jsonError(w, fmt.Sprintf("Unable to read entry data: %v", err), 400)
		return
	}
	if settings.TimeLimit != 0 && time.Now().Sub(entryData.CreatedAt) > time.Duration(settings.TimeLimit) {
		jsonError(w, "Thread is closed for new comments", 401)
		return
	}

	comment := &comments.RawComment{}
	decoder := json.NewDecoder(req.Body)
	if err := decoder.Decode(comment); err != nil {
		jsonError(w, fmt.Sprintf("Error decoding JSON body: %v", err), 422)
		return
	}

	for _, email := range settings.BannedEmails {
		if strings.Contains(comment.Email, email) || strings.Contains(comment.Body, email) || strings.Contains(comment.URL, email) {
			w.Header().Add("X-Banned", "Email-Banned")
			fmt.Fprintln(w, "{}")
			return
		}
	}

	for _, keyword := range settings.BannedKeywords {
		if strings.Contains(comment.Email, keyword) || strings.Contains(comment.Body, keyword) || strings.Contains(comment.URL, keyword) {
			w.Header().Add("X-Banned", "Keyword-Banned")
			fmt.Fprintln(w, "{}")
			return
		}
	}

	comment.IP = req.RemoteAddr
	comment.Date = time.Now().String()
	comment.ID = fmt.Sprintf("%v", time.Now().UnixNano())
	comment.Verified = s.verify(comment.Email, req)

	parts := strings.Split(config.API.Repository, "/")
	matches := threadRegexp.FindStringSubmatch(entryData.Thread)
	dir := matches[1] + "/" + matches[2] + "/" + matches[3]
	firstParagraph := strings.SplitAfterN(strings.ToLower(strings.TrimSpace(comment.Body[0:len(comment.Body)])), "\n", 1)[0]
	name := squeeze.ReplaceAllString(strings.Trim(slugify.ReplaceAllString(firstParagraph[0:Min(50, len(firstParagraph))], "-"), "-"), "-")

	pathname := path.Join(
		config.Threads.Source,
		dir,
		fmt.Sprintf("%v-%v.json", (time.Now().UnixNano()/1000000), name),
	)

	message := firstParagraph
	if len(message) > 255 {
		message = message[:255]
	}
	content, _ := json.Marshal(comment)
	branch := "master"

	if settings.RequireApproval || comment.IsSuspicious() {
		branch = "comment-" + comment.ID
		master, _, err := client.GetRepoBranch(parts[0], parts[1], "master")
		if err != nil {
			jsonError(w, fmt.Sprintf("Failed to write comment: %v", err), 500)
			return
		}

		_, _, err = client.CreateBranch(parts[0], parts[1], gitea.CreateBranchOption{
			BranchName:    branch,
			OldBranchName: "master",
		})
		if err != nil {
		}

		encodedContent := base64.StdEncoding.EncodeToString(content)
		_, _, err = client.CreateFile(parts[0], parts[1], pathname, gitea.CreateFileOptions{
			FileOptions: gitea.FileOptions{
				Message:       message,
				BranchName:    branch,
				NewBranchName: branch,
			},
			Content: encodedContent,
		})

		if err != nil {
			jsonError(w, fmt.Sprintf("Failed to write comment: %v", err), 500)
			return
		}

		pr := gitea.CreatePullRequestOption{
			Title: message,
			Head:  branch,
			Base:  master.Name,
			Body:  comment.Body,
		}
		_, _, err = client.CreatePullRequest(parts[0], parts[1], pr)
		if err != nil {
			jsonError(w, fmt.Sprintf("Failed to create PR: %v", err), 500)
			return
		}
	} else {
		encodedContent := base64.StdEncoding.EncodeToString(content)
		_, _, err := client.CreateFile(parts[0], parts[1], pathname, gitea.CreateFileOptions{
			FileOptions: gitea.FileOptions{
				Message:    message,
				BranchName: branch,
			},
			Content: encodedContent,
		})

		if err != nil {
			jsonError(w, fmt.Sprintf("Failed to write comment: %v", err), 500)
			return
		}
	}

	parsedComment := comments.ParseRaw(comment)
	response, _ := json.Marshal(parsedComment)
	w.Write(response)
}

func (s *Server) extractBearerToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", nil
	}

	matches := bearerRegexp.FindStringSubmatch(authHeader)
	if len(matches) != 2 {
		return "", fmt.Errorf("Invalid auth header format: %s", authHeader)
	}

	return matches[1], nil
}

func (s *Server) requireOperator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bearerToken, err := s.extractBearerToken(r)
		if err != nil || bearerToken == "" {
			jsonError(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if bearerToken == s.config.OperatorToken && s.config.OperatorToken != "" {
			// Authorized as Operator
			next.ServeHTTP(w, r)
			return
		}

		token, err := jwt.Parse(bearerToken, func(token *jwt.Token) (interface{}, error) {
			if token.Method.Alg() != jwt.SigningMethodHS256.Name {
				return nil, fmt.Errorf("Unexpected signing method: %v", token.Method.Alg())
			}
			return []byte(s.config.JWT.Secret), nil
		})
		if err != nil {
			jsonError(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
			appMetadata, ok := claims["app_metadata"].(map[string]interface{})
			if ok {
				roles, ok := appMetadata["roles"].([]interface{})
				if ok {
					for _, roleRaw := range roles {
						if roleStr, ok := roleRaw.(string); ok && roleStr == "admin" {
							next.ServeHTTP(w, r)
							return
						}
					}
				}
			}
		}

		jsonError(w, "Unauthorized operator token", http.StatusUnauthorized)
	})
}

func (s *Server) verify(email string, r *http.Request) bool {
	bearerToken, err := s.extractBearerToken(r)
	if err != nil || bearerToken == "" {
		logrus.Info("No or invalid auth header")
		return false
	}

	if bearerToken == s.config.OperatorToken && s.config.OperatorToken != "" {
		logrus.Info("Making operator request")
		return true
	}

	config := s.config
	if instance := getInstance(r.Context()); instance != nil {
		if instanceConfig, err := instance.Config(); err == nil && instanceConfig != nil {
			config = instanceConfig
		}
	}

	token, err := jwt.Parse(bearerToken, func(token *jwt.Token) (interface{}, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Name {
			return nil, fmt.Errorf("Unexpected signing method: %v", token.Method.Alg())
		}
		return []byte(config.JWT.Secret), nil
	})
	if err != nil {
		logrus.Errorf("Error verifying JWT: %v", err)
		return false
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		claimedEmail, ok := claims["email"]
		logrus.Infof("Checking email %v from claims %v against %v", claimedEmail, claims, email)
		return ok && claimedEmail == email
	}

	return false
}

// Index endpoint
func (s *Server) index(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	sendJSON(w, 200, map[string]string{
		"version":     s.version,
		"name":        "GoTell",
		"description": "GoTell is an API and build tool for handling large amounts of comments for JAMstack products",
	})
}

// ListenAndServe starts the Comments Server
func (s *Server) ListenAndServe() error {
	l := fmt.Sprintf("%v:%v", s.config.API.Host, s.config.API.Port)
	logrus.Infof("GoTell API started on: %s", l)
	return http.ListenAndServe(l, s.handler)
}

func NewServer(config *conf.Configuration, giteaClient *gitea.Client, db *gorm.DB) *Server {
	return NewServerWithVersion(config, giteaClient, db, defaultVersion)
}

func NewServerWithVersion(config *conf.Configuration, giteaClient *gitea.Client, db *gorm.DB, version string) *Server {
	s := &Server{
		config:  config,
		client:  giteaClient,
		db:      db,
		version: version,
	}

	mux := chi.NewRouter()
	mux.Use(middleware.RequestID)
	mux.Use(middleware.RealIP)
	mux.Use(middleware.Logger)
	mux.Use(middleware.Recoverer)

	corsHandler := cors.New(cors.Options{
		AllowedMethods:   []string{"GET", "POST", "PATCH", "PUT", "DELETE"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link", "X-Total-Count"},
		AllowCredentials: true,
	})

	mux.Use(corsHandler.Handler)
	mux.Use(timeRequestChi)

	mux.Get("/", func(w http.ResponseWriter, r *http.Request) {
		s.index(r.Context(), w, r)
	})

	mux.With(s.requireOperator).Post("/instances", s.CreateInstance)

	mux.Route("/instances/{instance_id}", func(r chi.Router) {
		r.Use(s.instanceMiddleware)
		r.With(s.requireOperator).Get("/", s.GetInstance)
		r.With(s.requireOperator).Put("/", s.UpdateInstance)
		r.With(s.requireOperator).Delete("/", s.DeleteInstance)

		r.With(jsonTypeRequiredChi).Post("/*", s.postComment)
	})

	// Legacy route support without instance
	mux.With(jsonTypeRequiredChi).Post("/*", s.postComment)

	s.handler = mux
	return s
}

func timeRequestChi(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), contextKey("_gotell_timing"), time.Now())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func logHandler(ctx context.Context, wp mutil.WriterProxy, req *http.Request) {
	start := ctx.Value(contextKey("_gotell_timing")).(time.Time)
	logrus.WithFields(logrus.Fields{
		"method":   req.Method,
		"path":     req.URL.Path,
		"status":   wp.Status(),
		"duration": time.Since(start),
	}).Info("")
}

func jsonTypeRequiredChi(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "Content-Type must be application/json", 422)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sendJSON(w http.ResponseWriter, status int, obj interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.Encode(obj)
}

func jsonError(w http.ResponseWriter, message string, status int) {
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.Encode(map[string]string{"msg": message})
}
