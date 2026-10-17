package domain

type ImageFeatures struct {
	Subject     string `json:"subject"`
	Action      string `json:"action,omitempty"`
	Scene       string `json:"scene"`
	Composition string `json:"composition"`
	Palette     string `json:"palette"`
	Lighting    string `json:"lighting"`
	Texture     string `json:"texture"`
	Text        string `json:"text"`
}
type ImageAnalysis struct {
	AssetID        string        `json:"asset_id"`
	Width          int           `json:"width"`
	Height         int           `json:"height"`
	Features       ImageFeatures `json:"features"`
	SHA256         string        `json:"sha256,omitempty"`
	RecognizedText string        `json:"recognized_text,omitempty"`
	TextError      string        `json:"text_error,omitempty"`
}
type ImageReference struct {
	AssetID   string `json:"asset_id"`
	LibraryID string `json:"library_id,omitempty"`
	Title     string `json:"title,omitempty"`
}
type ImageOutput struct {
	Ratio   string `json:"ratio"`
	Clarity string `json:"clarity"`
	Count   int    `json:"count"`
}

// ImageEditIntent separates content preservation from appearance changes.
// Empty mode keeps the behavior of previously saved feature-mix schemes.
type ImageEditIntent struct {
	Mode    string `json:"mode"`
	Style   string `json:"style"`
	Palette string `json:"palette"`
}

// ImageFeatureControl describes one independent, user-confirmed dimension.
// Image controls borrow that dimension only; custom and auto carry no image.
type ImageFeatureControl struct {
	Mode    string `json:"mode"`
	AssetID string `json:"asset_id,omitempty"`
	Value   string `json:"value,omitempty"`
}

// ImageProposal is an analyzed editing direction. Its controls are applied
// only after the user selects this proposal, before prompt composition.
type ImageProposal struct {
	ID              string                         `json:"id"`
	Title           string                         `json:"title"`
	Summary         string                         `json:"summary"`
	FeatureControls map[string]ImageFeatureControl `json:"feature_controls"`
}
type ImageBatchItem struct {
	Index   int    `json:"index"`
	JobID   string `json:"job_id,omitempty"`
	State   string `json:"state"`
	AssetID string `json:"asset_id,omitempty"`
	Error   string `json:"error,omitempty"`
}
type ImageBatch struct {
	SelectedProposal   *ImageProposal                 `json:"selected_proposal,omitempty"`
	ExtraRequirements  string                         `json:"extra_requirements,omitempty"`
	FeatureControls    map[string]ImageFeatureControl `json:"feature_controls,omitempty"`
	EditIntent         ImageEditIntent                `json:"edit_intent,omitempty"`
	ID                 string                         `json:"id"`
	RequestID          string                         `json:"request_id"`
	Revision           int                            `json:"revision"`
	State              string                         `json:"state"`
	Items              []ImageBatchItem               `json:"items"`
	Output             ImageOutput                    `json:"output"`
	Prompt             string                         `json:"prompt"`
	PrimaryAssetID     string                         `json:"primary_asset_id"`
	InputAssetIDs      []string                       `json:"input_asset_ids"`
	InputHashes        []string                       `json:"input_hashes"`
	InputDigest        string                         `json:"input_digest"`
	FeatureSources     map[string]string              `json:"feature_sources,omitempty"`
	PromptRulesVersion int                            `json:"prompt_rules_version,omitempty"`
	Model              string                         `json:"model"`
	CreatedAt          string                         `json:"created_at"`
}
type ImageStyle struct {
	Preset      string   `json:"preset"`
	Custom      string   `json:"custom"`
	Palette     string   `json:"palette"`
	Composition string   `json:"composition"`
	Lighting    string   `json:"lighting"`
	Typography  string   `json:"typography"`
	Ratio       string   `json:"ratio"`
	Retain      []string `json:"retain"`
}
type ImageResult struct {
	SelectedProposal   *ImageProposal                 `json:"selected_proposal,omitempty"`
	ExtraRequirements  string                         `json:"extra_requirements,omitempty"`
	FeatureControls    map[string]ImageFeatureControl `json:"feature_controls,omitempty"`
	EditIntent         ImageEditIntent                `json:"edit_intent,omitempty"`
	FeatureSources     map[string]string              `json:"feature_sources,omitempty"`
	PromptRulesVersion int                            `json:"prompt_rules_version,omitempty"`
	BatchID            string                         `json:"batch_id,omitempty"`
	PrimaryAssetID     string                         `json:"primary_asset_id,omitempty"`
	InputHashes        []string                       `json:"input_hashes,omitempty"`
	Width              int                            `json:"width,omitempty"`
	Height             int                            `json:"height,omitempty"`
	Warning            string                         `json:"warning,omitempty"`
	ProviderRequestID  string                         `json:"provider_request_id,omitempty"`
	JobID              string                         `json:"job_id,omitempty"`
	AssetID            string                         `json:"asset_id"`
	Revision           int                            `json:"revision"`
	Prompt             string                         `json:"prompt"`
	Model              string                         `json:"model"`
	CreatedAt          string                         `json:"created_at"`
	Ratio              string                         `json:"ratio,omitempty"`
	SourceAssetIDs     []string                       `json:"source_asset_ids,omitempty"`
	StyleReferenceID   string                         `json:"style_reference_id,omitempty"`
}
type ImageRewrite struct {
	Proposals            []ImageProposal                `json:"proposals,omitempty"`
	ProposalsInputDigest string                         `json:"proposals_input_digest,omitempty"`
	SelectedProposalID   string                         `json:"selected_proposal_id,omitempty"`
	ExtraRequirements    string                         `json:"extra_requirements,omitempty"`
	FeatureControls      map[string]ImageFeatureControl `json:"feature_controls,omitempty"`
	EditIntent           ImageEditIntent                `json:"edit_intent,omitempty"`
	WorkflowVersion      int                            `json:"workflow_version,omitempty"`
	PrimaryAssetID       string                         `json:"primary_asset_id,omitempty"`
	References           []ImageReference               `json:"references,omitempty"`
	FeatureSources       map[string]string              `json:"feature_sources,omitempty"`
	Output               ImageOutput                    `json:"output,omitempty"`
	TextMode             string                         `json:"text_mode,omitempty"`
	PromptInputDigest    string                         `json:"prompt_input_digest,omitempty"`
	PromptImageConfig    string                         `json:"prompt_image_config,omitempty"`
	PromptRulesVersion   int                            `json:"prompt_rules_version,omitempty"`
	Batches              []ImageBatch                   `json:"batches,omitempty"`
	ID                   string                         `json:"id"`
	VersionID            string                         `json:"version_id"`
	AppliedVersionID     string                         `json:"applied_version_id,omitempty"`
	SourceAssetIDs       []string                       `json:"source_asset_ids"`
	StyleReferenceID     string                         `json:"style_reference_id,omitempty"`
	StyleLibraryID       string                         `json:"style_library_id,omitempty"`
	StyleReferenceTitle  string                         `json:"style_reference_title,omitempty"`
	Analyses             []ImageAnalysis                `json:"analyses"`
	Style                ImageStyle                     `json:"style"`
	Brief                string                         `json:"brief,omitempty"`
	ImageText            string                         `json:"image_text,omitempty"`
	Prompt               string                         `json:"prompt"`
	Revision             int                            `json:"revision"`
	Status               string                         `json:"status"`
	Error                string                         `json:"error,omitempty"`
	Results              []ImageResult                  `json:"results"`
	CreatedAt            string                         `json:"created_at"`
	UpdatedAt            string                         `json:"updated_at"`
}
type ImageVoiceOptions struct {
	ImagePolicy   string   `json:"image_policy"`
	Style         string   `json:"style"`
	SceneCount    int      `json:"scene_count"`
	TargetSeconds int      `json:"target_seconds"`
	ImageAssetIDs []string `json:"image_asset_ids"`
}
type ImageVoicePlan struct {
	Script    string            `json:"script"`
	Options   ImageVoiceOptions `json:"options"`
	Model     string            `json:"model"`
	CreatedAt string            `json:"created_at"`
}
