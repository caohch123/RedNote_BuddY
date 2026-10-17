package domain

import "encoding/json"

type Author struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url,omitempty"`
}
type Note struct {
	ID               string         `json:"id"`
	Platform         string         `json:"platform"`
	NoteID           string         `json:"note_id"`
	URL              string         `json:"url"`
	Title            string         `json:"title"`
	Body             string         `json:"body"`
	Topics           []string       `json:"topics"`
	Author           Author         `json:"author"`
	Type             string         `json:"type"`
	Likes            *int64         `json:"likes"`
	Collects         *int64         `json:"collects"`
	Comments         *int64         `json:"comments"`
	Shares           *int64         `json:"shares,omitempty"`
	PublishedAt      string         `json:"published_at"`
	CollectedAt      string         `json:"collected_at"`
	Tags             []string       `json:"tags"`
	Status           string         `json:"status"`
	MetadataStatus   string         `json:"metadata_status"`
	MediaStatus      string         `json:"media_status"`
	ExtractionStatus string         `json:"extraction_status"`
	Error            string         `json:"error,omitempty"`
	AssetIDs         []string       `json:"asset_ids"`
	CoverAssetID     string         `json:"cover_asset_id,omitempty"`
	Raw              map[string]any `json:"raw,omitempty"`
}
type Segment struct {
	Text  string  `json:"text"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}
type Extraction struct {
	Text     string    `json:"text"`
	Segments []Segment `json:"segments"`
	Status   string    `json:"status"`
	Error    string    `json:"error,omitempty"`
	Edited   bool      `json:"edited"`
}
type Asset struct {
	ID               string     `json:"id"`
	Name             string     `json:"name,omitempty"`
	LibraryDeletedAt string     `json:"library_deleted_at,omitempty"`
	OwnerID          string     `json:"owner_id"`
	Kind             string     `json:"kind"`
	SourceURL        string     `json:"source_url,omitempty"`
	SourceID         string     `json:"source_id,omitempty"`
	ExpectedSize     int64      `json:"expected_size,omitempty"`
	Path             string     `json:"-"`
	LocalPath        string     `json:"local_path,omitempty"`
	MIME             string     `json:"mime"`
	Status           string     `json:"status"`
	Error            string     `json:"error,omitempty"`
	Position         int        `json:"position"`
	Width            int        `json:"width"`
	Height           int        `json:"height"`
	Duration         float64    `json:"duration"`
	SHA256           string     `json:"sha256,omitempty"`
	Size             int64      `json:"size"`
	CreatedAt        string     `json:"created_at"`
	Extraction       Extraction `json:"extraction"`
}
type RewriteOptions struct {
	Audience           string   `json:"audience"`
	Tone               string   `json:"tone"`
	Length             string   `json:"length"`
	Instructions       string   `json:"instructions"`
	IncludeBody        bool     `json:"include_body"`
	ExtractionAssetIDs []string `json:"extraction_asset_ids"`
}
type Version struct {
	ID                string                `json:"id"`
	Number            int                   `json:"number"`
	Title             string                `json:"title"`
	Body              string                `json:"body"`
	Topics            []string              `json:"topics"`
	Script            string                `json:"script"`
	AssetIDs          []string              `json:"asset_ids"`
	CreatedAt         string                `json:"created_at"`
	Confirmed         bool                  `json:"confirmed"`
	SourceSnapshot    *Note                 `json:"source_snapshot,omitempty"`
	ParentVersionID   string                `json:"parent_version_id,omitempty"`
	SourceExtractions map[string]Extraction `json:"source_extractions,omitempty"`
}
type Rewrite struct {
	ImageRewrites      []ImageRewrite `json:"image_rewrites,omitempty"`
	ID                 string         `json:"id"`
	NoteID             string         `json:"note_id"`
	Title              string         `json:"title"`
	TitleCustomized    bool           `json:"title_customized,omitempty"`
	Pinned             bool           `json:"pinned,omitempty"`
	Versions           []Version      `json:"versions"`
	ConfirmedVersionID string         `json:"confirmed_version_id"`
	CreatedAt          string         `json:"created_at"`
	UpdatedAt          string         `json:"updated_at"`
	Options            RewriteOptions `json:"options"`
}
type Voice struct {
	ID       string  `json:"id"`
	Text     string  `json:"text"`
	AssetID  string  `json:"asset_id"`
	Duration float64 `json:"duration"`
}
type Scene struct {
	ID           string  `json:"id"`
	Position     int     `json:"position"`
	Prompt       string  `json:"prompt"`
	Narration    string  `json:"narration"`
	Duration     int     `json:"duration"`
	VideoAssetID string  `json:"video_asset_id,omitempty"`
	RemoteTaskID string  `json:"remote_task_id,omitempty"`
	Status       string  `json:"status"`
	Error        string  `json:"error,omitempty"`
	Voices       []Voice `json:"voices"`
	Fingerprint  string  `json:"fingerprint,omitempty"`
	ImageAssetID string  `json:"image_asset_id,omitempty"`
	ImageSource  string  `json:"image_source,omitempty"`
}
type RenderVersion struct {
	ID           string `json:"id"`
	Revision     int    `json:"revision"`
	AssetID      string `json:"asset_id"`
	CoverAssetID string `json:"cover_asset_id"`
	SRTAssetID   string `json:"srt_asset_id"`
	CreatedAt    string `json:"created_at"`
}

// Legacy scene/plan/render fields are inert archive metadata for file and publication references.
// Only VideoRewrite is an executable video workflow.
type VideoProject struct {
	VideoRewrite *VideoRewriteState `json:"video_rewrite,omitempty"`
	Plan         *ImageVoicePlan    `json:"plan,omitempty"`
	PlanHistory  []ImageVoicePlan   `json:"plan_history,omitempty"`
	ID           string             `json:"id"`
	RewriteID    string             `json:"rewrite_id"`
	VersionID    string             `json:"version_id"`
	Title        string             `json:"title"`
	Duration     int                `json:"duration"`
	Revision     int                `json:"revision"`
	Scenes       []Scene            `json:"scenes"`
	Renders      []RenderVersion    `json:"renders"`
	Status       string             `json:"status"`
	Stale        bool               `json:"stale"`
	CreatedAt    string             `json:"created_at"`
	UpdatedAt    string             `json:"updated_at"`
	Workflow     string             `json:"workflow,omitempty"`
}
type Publication struct {
	ID                 string   `json:"id"`
	TargetPlatform     string   `json:"target_platform"`
	PlatformWorkID     string   `json:"platform_work_id,omitempty"`
	SubmittedAt        string   `json:"submitted_at,omitempty"`
	PrepareJobID       string   `json:"prepare_job_id,omitempty"`
	RewriteID          string   `json:"rewrite_id"`
	VersionID          string   `json:"version_id"`
	Title              string   `json:"title"`
	Body               string   `json:"body"`
	Topics             []string `json:"topics"`
	AssetIDs           []string `json:"asset_ids"`
	VideoProjectID     string   `json:"video_project_id,omitempty"`
	VideoRevision      int      `json:"video_revision,omitempty"`
	Status             string   `json:"status"`
	CreatedAt          string   `json:"created_at"`
	PublishedAt        string   `json:"published_at,omitempty"`
	PublishedURL       string   `json:"published_url,omitempty"`
	PublishJobID       string   `json:"publish_job_id,omitempty"`
	PublishError       string   `json:"publish_error,omitempty"`
	PublishAccountID   string   `json:"publish_account_id,omitempty"`
	PublishAccountName string   `json:"publish_account_name,omitempty"`
	PublishMethod      string   `json:"publish_method,omitempty"`
	XHSNoteID          string   `json:"xhs_note_id,omitempty"`
	Visibility         string   `json:"visibility,omitempty"`
}
type ProviderConfig struct {
	BaseURL     string `json:"base_url"`
	Model       string `json:"model"`
	APIKey      string `json:"api_key,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Configured  bool   `json:"configured"`
	KeyMask     string `json:"key_mask,omitempty"`
}
type Settings struct {
	CollectorURL     string                    `json:"collector_url"`
	Cookie           string                    `json:"cookie,omitempty"`
	CookieConfigured bool                      `json:"cookie_configured"`
	CookieMask       string                    `json:"cookie_mask,omitempty"`
	Providers        map[string]ProviderConfig `json:"providers"`
	FFmpegPath       string                    `json:"ffmpeg_path"`
	FFprobePath      string                    `json:"ffprobe_path"`
	NodePath         string                    `json:"node_path"`
}
type Job struct {
	ID                      string                      `json:"id"`
	Kind                    string                      `json:"kind"`
	EntityID                string                      `json:"entity_id"`
	AssetID                 string                      `json:"asset_id,omitempty"`
	SceneID                 string                      `json:"scene_id,omitempty"`
	State                   string                      `json:"state"`
	Phase                   string                      `json:"phase"`
	Progress                int                         `json:"progress"`
	Message                 string                      `json:"message,omitempty"`
	Error                   string                      `json:"error,omitempty"`
	CreatedAt               string                      `json:"created_at"`
	UpdatedAt               string                      `json:"updated_at"`
	Attempts                int                         `json:"attempts"`
	Payload                 json.RawMessage             `json:"payload,omitempty"`
	ProviderSnapshot        map[string]ProviderConfig   `json:"provider_snapshot,omitempty"`
	ProviderSnapshotHistory []map[string]ProviderConfig `json:"provider_snapshot_history,omitempty"`
	RemoteTaskID            string                      `json:"remote_task_id,omitempty"`
	RemoteTaskIDs           []string                    `json:"remote_task_ids,omitempty"`
	InputScene              *Scene                      `json:"input_scene,omitempty"`
	SourceVersionID         string                      `json:"source_version_id,omitempty"`
	ResultAssetIDs          []string                    `json:"result_asset_ids,omitempty"`
}
