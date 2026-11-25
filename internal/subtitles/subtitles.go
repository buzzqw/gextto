// Package subtitles implements the small OpenSubtitles-compatible REST surface
// Gextto needs: log in, search, resolve one file and download its bytes.
package subtitles

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultBaseURL   = "https://api.opensubtitles.com/api/v1"
	maxSubtitleBytes = 4 << 20
)

type Config struct {
	BaseURL   string
	APIKey    string
	Username  string
	Password  string
	UserAgent string
	Client    *http.Client
}

type Client struct {
	cfg     Config
	mu      sync.Mutex
	loginMu sync.Mutex
	token   string
	baseURL string
}

type flexibleID int64

func (id *flexibleID) UnmarshalJSON(data []byte) error {
	value := strings.TrimSpace(string(data))
	if value == "" || value == "null" {
		*id = 0
		return nil
	}
	if strings.HasPrefix(value, "\"") {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		value = text
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid numeric id %q", value)
	}
	*id = flexibleID(parsed)
	return nil
}

type SearchQuery struct {
	Language  string
	Kind      string // movie or episode
	Title     string
	Year      int
	IMDbID    string
	TMDBID    string
	Season    int
	Episode   int
	MovieHash string
	MovieSize int64
}

type File struct {
	ID   flexibleID `json:"file_id"`
	Name string     `json:"file_name"`
}

type Subtitle struct {
	ID             flexibleID `json:"id"`
	SubtitleID     flexibleID `json:"subtitle_id"`
	Language       string     `json:"language"`
	ReleaseName    string     `json:"release"`
	Releases       []string   `json:"releases"`
	Files          []File     `json:"files"`
	HashMatch      bool       `json:"moviehash_match"`
	SizeMatch      bool       `json:"moviebytesize_match"`
	IMDbMatch      bool       `json:"imdbid_match"`
	Title          string     `json:"title"`
	Year           int        `json:"year"`
	Season         int        `json:"season"`
	Episode        int        `json:"episode"`
	FeatureDetails struct {
		Title     string `json:"title"`
		MovieName string `json:"movie_name"`
		Year      int    `json:"year"`
		IMDbID    string `json:"imdb_id"`
		Season    int    `json:"season_number"`
		Episode   int    `json:"episode_number"`
	} `json:"feature_details"`
	DownloadCount     int   `json:"download_count"`
	HearingImpaired   *bool `json:"hearing_impaired"`
	ForeignPartsOnly  *bool `json:"foreign_parts_only"`
	FromTrusted       *bool `json:"from_trusted"`
	AITranslated      *bool `json:"ai_translated"`
	MachineTranslated *bool `json:"machine_translated"`
}

func (s Subtitle) Release() string {
	if len(s.Releases) > 0 {
		return s.Releases[0]
	}
	return s.ReleaseName
}

func (s Subtitle) PrimaryFile() (File, bool) {
	for _, file := range s.Files {
		if file.ID > 0 {
			return file, true
		}
	}
	return File{}, false
}

type searchResponse struct {
	Data []struct {
		ID         flexibleID `json:"id"`
		Attributes Subtitle   `json:"attributes"`
	} `json:"data"`
}

type Download struct {
	Data      []byte
	FileName  string
	Remaining int
}

func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("invalid subtitle provider URL")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("subtitle provider API key is required")
	}
	username, password := strings.TrimSpace(cfg.Username), strings.TrimSpace(cfg.Password)
	if (username == "") != (password == "") {
		return nil, errors.New("subtitle provider username and password must be supplied together")
	}
	cfg.Username, cfg.Password = username, password
	if strings.TrimSpace(cfg.UserAgent) == "" {
		cfg.UserAgent = "Gextto"
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errors.New("too many redirects")
				}
				if len(via) > 0 && !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
					req.Header.Del("Api-Key")
					req.Header.Del("Authorization")
				}
				return nil
			},
		}
	}
	return &Client{cfg: cfg, baseURL: cfg.BaseURL}, nil
}

func (c *Client) endpoint(path string) string {
	c.mu.Lock()
	base := c.baseURL
	c.mu.Unlock()
	if base == "" {
		base = c.cfg.BaseURL
	}
	return base + path
}

func (c *Client) request(ctx context.Context, method, path string, body any, token bool) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(path), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Api-Key", c.cfg.APIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token {
		c.mu.Lock()
		value := c.token
		c.mu.Unlock()
		if value != "" {
			req.Header.Set("Authorization", "Bearer "+value)
		}
	}
	return c.cfg.Client.Do(req)
}

func (c *Client) login(ctx context.Context) error {
	if c.cfg.Username == "" || c.cfg.Password == "" {
		return nil
	}
	c.loginMu.Lock()
	defer c.loginMu.Unlock()
	c.mu.Lock()
	hasToken := c.token != ""
	c.mu.Unlock()
	if hasToken {
		return nil
	}
	response, err := c.request(ctx, http.MethodPost, "/login", map[string]string{
		"username": c.cfg.Username,
		"password": c.cfg.Password,
	}, false)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("subtitle provider login returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Token   string `json:"token"`
		BaseURL string `json:"base_url"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&payload); err != nil {
		return fmt.Errorf("read subtitle provider login: %w", err)
	}
	if strings.TrimSpace(payload.Token) == "" {
		return errors.New("subtitle provider login returned no token")
	}
	c.mu.Lock()
	c.token = strings.TrimSpace(payload.Token)
	if base := normalizeBaseURL(payload.BaseURL); base != "" {
		c.baseURL = base
	}
	c.mu.Unlock()
	return nil
}

func normalizeBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	return strings.TrimRight(raw, "/")
}

func (c *Client) authorized(ctx context.Context) error {
	c.mu.Lock()
	hasToken := c.token != ""
	c.mu.Unlock()
	if hasToken {
		return nil
	}
	return c.login(ctx)
}

func (c *Client) Search(ctx context.Context, query SearchQuery) ([]Subtitle, error) {
	if err := c.authorized(ctx); err != nil {
		return nil, err
	}
	values := url.Values{}
	if language := strings.TrimSpace(query.Language); language != "" {
		values.Set("languages", language)
	}
	if kind := strings.TrimSpace(query.Kind); kind != "" {
		values.Set("type", kind)
	}
	if title := strings.TrimSpace(query.Title); title != "" {
		values.Set("query", title)
	}
	if query.Year > 0 {
		values.Set("year", strconv.Itoa(query.Year))
	}
	if id := strings.TrimPrefix(strings.TrimSpace(query.IMDbID), "tt"); id != "" {
		values.Set("imdb_id", id)
	}
	if id := strings.TrimSpace(query.TMDBID); id != "" {
		values.Set("tmdb_id", id)
	}
	if query.Season > 0 {
		values.Set("season_number", strconv.Itoa(query.Season))
	}
	if query.Episode > 0 {
		values.Set("episode_number", strconv.Itoa(query.Episode))
	}
	if hash := strings.TrimSpace(query.MovieHash); hash != "" {
		values.Set("moviehash", hash)
	}
	if query.MovieSize > 0 {
		values.Set("moviebytesize", strconv.FormatInt(query.MovieSize, 10))
	}
	values.Set("page", "1")
	path := "/subtitles?" + values.Encode()
	response, err := c.request(ctx, http.MethodGet, path, nil, true)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		if err := c.login(ctx); err != nil {
			return nil, err
		}
		response, err = c.request(ctx, http.MethodGet, path, nil, true)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("subtitle provider search returned HTTP %d", response.StatusCode)
	}
	var payload searchResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("read subtitle provider search: %w", err)
	}
	result := make([]Subtitle, 0, len(payload.Data))
	for _, item := range payload.Data {
		subtitle := item.Attributes
		if item.ID > 0 {
			subtitle.ID = item.ID
		}
		if subtitle.ID == 0 {
			subtitle.ID = subtitle.SubtitleID
		}
		if subtitle.Title == "" {
			subtitle.Title = subtitle.FeatureDetails.Title
			if subtitle.Title == "" {
				subtitle.Title = subtitle.FeatureDetails.MovieName
			}
		}
		if subtitle.Year == 0 {
			subtitle.Year = subtitle.FeatureDetails.Year
		}
		if subtitle.Season == 0 {
			subtitle.Season = subtitle.FeatureDetails.Season
		}
		if subtitle.Episode == 0 {
			subtitle.Episode = subtitle.FeatureDetails.Episode
		}
		result = append(result, subtitle)
	}
	return result, nil
}

func (c *Client) Download(ctx context.Context, fileID int64, format string) (Download, error) {
	if fileID <= 0 {
		return Download{}, errors.New("subtitle file id is required")
	}
	if strings.TrimSpace(format) == "" {
		format = "srt"
	}
	if err := c.authorized(ctx); err != nil {
		return Download{}, err
	}
	response, err := c.request(ctx, http.MethodPost, "/download", map[string]any{"file_id": fileID, "sub_format": strings.ToLower(strings.TrimSpace(format))}, true)
	if err != nil {
		return Download{}, err
	}
	if response.StatusCode == http.StatusUnauthorized {
		response.Body.Close()
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		if err := c.login(ctx); err != nil {
			return Download{}, err
		}
		response, err = c.request(ctx, http.MethodPost, "/download", map[string]any{"file_id": fileID, "sub_format": strings.ToLower(strings.TrimSpace(format))}, true)
		if err != nil {
			return Download{}, err
		}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Download{}, fmt.Errorf("subtitle provider download returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Link      string `json:"link"`
		FileName  string `json:"file_name"`
		Remaining int    `json:"remaining"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&payload); err != nil {
		return Download{}, fmt.Errorf("read subtitle provider download: %w", err)
	}
	if strings.TrimSpace(payload.Link) == "" {
		return Download{}, errors.New("subtitle provider returned no download link")
	}
	link, err := url.Parse(payload.Link)
	if err != nil || (link.Scheme != "http" && link.Scheme != "https") {
		return Download{}, errors.New("invalid subtitle download link")
	}
	if !link.IsAbs() {
		base, parseErr := url.Parse(c.endpoint(""))
		if parseErr != nil {
			return Download{}, parseErr
		}
		link = base.ResolveReference(link)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link.String(), nil)
	if err != nil {
		return Download{}, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	fileResponse, err := c.cfg.Client.Do(req)
	if err != nil {
		return Download{}, err
	}
	defer fileResponse.Body.Close()
	if fileResponse.StatusCode < 200 || fileResponse.StatusCode >= 300 {
		return Download{}, fmt.Errorf("subtitle file download returned HTTP %d", fileResponse.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(fileResponse.Body, maxSubtitleBytes+1))
	if err != nil {
		return Download{}, err
	}
	if len(data) > maxSubtitleBytes {
		return Download{}, errors.New("subtitle file is too large")
	}
	if len(data) == 0 {
		return Download{}, errors.New("subtitle file is empty")
	}
	name := strings.TrimSpace(payload.FileName)
	if name == "" {
		name = "subtitle.srt"
	}
	name = filepathBase(name)
	return Download{Data: data, FileName: name, Remaining: payload.Remaining}, nil
}

func filepathBase(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if index := strings.LastIndexByte(name, '/'); index >= 0 {
		return name[index+1:]
	}
	return name
}

// HashFile computes the OpenSubtitles OSHash used by the REST API: the file
// size plus the sum of the little-endian 64-bit words in the first and last
// 64 KiB, wrapping at 64 bits. It reads at most 128 KiB and returns 16 hex
// characters. OSHash is an identity hint, not an integrity check.
func HashFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", 0, err
	}
	const blockSize = 64 << 10
	if !info.Mode().IsRegular() || info.Size() < 2*blockSize {
		return "", 0, errors.New("OSHash requires a regular video of at least 128 KiB")
	}
	hash := uint64(info.Size())
	readSum := func() error {
		buffer := make([]byte, blockSize)
		if _, err := io.ReadFull(file, buffer); err != nil {
			return err
		}
		for offset := 0; offset < len(buffer); offset += 8 {
			hash += binary.LittleEndian.Uint64(buffer[offset : offset+8])
		}
		return nil
	}
	if err := readSum(); err != nil {
		return "", 0, err
	}
	if _, err := file.Seek(info.Size()-blockSize, io.SeekStart); err != nil {
		return "", 0, err
	}
	if err := readSum(); err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("%016x", hash), info.Size(), nil
}
