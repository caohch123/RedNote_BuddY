package domain

type VideoOutput struct {
	Duration      int    `json:"duration"`
	Resolution    string `json:"resolution"`
	Ratio         string `json:"ratio"`
	GenerateAudio bool   `json:"generate_audio"`
}

type VideoScriptSegment struct {
	Start  int    `json:"start"`
	End    int    `json:"end"`
	Visual string `json:"visual"`
	Action string `json:"action"`
	Camera string `json:"camera"`
	Audio  string `json:"audio"`
}

type VideoMaterialAnalysis struct {
	AssetID        string            `json:"asset_id"`
	SHA256         string            `json:"sha256"`
	Kind           string            `json:"kind"`
	ModelSignature string            `json:"model_signature"`
	RulesVersion   int               `json:"rules_version"`
	Features       map[string]string `json:"features"`
	Transcript     string            `json:"transcript,omitempty"`
}

type VideoGenerationSnapshot struct {
	Provider       string                  `json:"provider,omitempty"`
	Analyses       []VideoMaterialAnalysis `json:"analyses,omitempty"`
	PrimaryAssetID string                  `json:"primary_asset_id,omitempty"`
	InputAssetIDs  []string                `json:"input_asset_ids"`
	InputHashes    map[string]string       `json:"input_hashes"`
	VideoURLs      map[string]string       `json:"video_urls,omitempty"`
	PurposeSources map[string]string       `json:"purpose_sources"`
	Requirements   string                  `json:"requirements"`
	Output         VideoOutput             `json:"output"`
	Script         []VideoScriptSegment    `json:"script"`
	Prompt         string                  `json:"prompt"`
	InputDigest    string                  `json:"input_digest"`
	ModelSignature string                  `json:"model_signature"`
	Model          string                  `json:"model"`
	Revision       int                     `json:"revision"`
}

type VideoSubmission struct {
	ReferenceUploads   map[string]VideoReferenceUpload `json:"reference_uploads,omitempty"`
	ProviderResolution int                             `json:"provider_resolution,omitempty"`
	ProviderSnapshot   map[string]ProviderConfig       `json:"provider_snapshot,omitempty"`
	ID                 string                          `json:"id"`
	RequestID          string                          `json:"request_id"`
	JobID              string                          `json:"job_id"`
	State              string                          `json:"state"`
	RemoteTaskID       string                          `json:"remote_task_id,omitempty"`
	ResultURL          string                          `json:"result_url,omitempty"`
	Snapshot           VideoGenerationSnapshot         `json:"snapshot"`
	CreatedAt          string                          `json:"created_at"`
}

// Upload receipts belong to the immutable submission's model and input hashes.
// They are reusable until expiry; they never imply that generation was submitted.
type VideoReferenceUpload struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

type VideoRewriteResult struct {
	AssetID        string                  `json:"asset_id"`
	PreviewAssetID string                  `json:"preview_asset_id,omitempty"`
	CoverAssetID   string                  `json:"cover_asset_id,omitempty"`
	JobID          string                  `json:"job_id"`
	SubmissionID   string                  `json:"submission_id"`
	Snapshot       VideoGenerationSnapshot `json:"snapshot"`
	Warning        string                  `json:"warning,omitempty"`
	CreatedAt      string                  `json:"created_at"`
}

type VideoRewriteState struct {
	ScriptRulesVersion     int                     `json:"script_rules_version,omitempty"`
	InitialSourceAssetID   string                  `json:"initial_source_asset_id,omitempty"`
	PrimaryAssetID         string                  `json:"primary_asset_id"`
	ReferenceVideoID       string                  `json:"reference_video_id"`
	ReferenceImageID       string                  `json:"reference_image_id"`
	VideoURLs              map[string]string       `json:"video_urls"`
	PurposeSources         map[string]string       `json:"purpose_sources"`
	Requirements           string                  `json:"requirements"`
	Output                 VideoOutput             `json:"output"`
	Analyses               []VideoMaterialAnalysis `json:"analyses"`
	Script                 []VideoScriptSegment    `json:"script"`
	Prompt                 string                  `json:"prompt"`
	ScriptInputDigest      string                  `json:"script_input_digest"`
	ScriptModelSignature   string                  `json:"script_model_signature"`
	Submissions            []VideoSubmission       `json:"submissions"`
	Results                []VideoRewriteResult    `json:"results"`
	InitializationComplete bool                    `json:"initialization_complete"`
	AppliedVersionID       string                  `json:"applied_version_id,omitempty"`
}
