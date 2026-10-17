export interface List<T> { items: T[]; total: number }
export interface Author { id: string; name: string; avatar_url?: string }
export interface Note {
  id: string; platform: string; note_id: string; url: string; title: string; body: string;
  topics: string[]; author: Author; type: string; likes: number | null; collects: number | null;
  comments: number | null; shares?: number | null; published_at: string; collected_at: string; tags: string[];
  status: string; metadata_status: string; media_status: string; extraction_status: string;
  error?: string; asset_ids: string[]; cover_asset_id?: string;
}
export interface Segment { text: string; start: number; end: number }
export interface Asset {
	name?: string; library_deleted_at?: string;
  id: string; owner_id: string; kind: string; source_url?: string; mime: string;
  status: string; error?: string; position: number; width: number; height: number;
  duration: number; size: number; created_at: string;
  extraction: { text: string; segments: Segment[]; status: string; error?: string; edited: boolean };
}
export interface RewriteOptions {
  audience: string; tone: string; length: string; instructions: string;
  include_body: boolean; extraction_asset_ids: string[];
}
export interface Version {
  id: string; number: number; title: string; body: string; topics: string[]; script: string;
  asset_ids: string[]; created_at: string; confirmed: boolean;
  parent_version_id?: string; source_snapshot?: Note; source_extractions?: Record<string, Asset['extraction']>;
}
export interface Rewrite {
	image_rewrites?: ImageRewrite[];
  id: string; note_id: string; title: string; versions: Version[]; confirmed_version_id: string;
  title_customized?: boolean; pinned?: boolean;
  created_at: string; updated_at: string; options: RewriteOptions;
}
export interface VideoProject {
	video_rewrite?: VideoRewriteState;
  id: string; rewrite_id: string; version_id: string; title: string; duration: number; revision: number;
  status: string; stale: boolean; created_at: string; updated_at: string;
  workflow?: string;
}
export type VideoPurpose = 'content' | 'subject' | 'action' | 'camera' | 'style' | 'audio';
export interface VideoOutput { duration: number; resolution: string; ratio: string; generate_audio: boolean }
export interface VideoScriptSegment { start: number; end: number; visual: string; action: string; camera: string; audio: string }
export interface VideoGenerationSnapshot {
  provider?: string;
  primary_asset_id?: string; input_asset_ids: string[]; input_hashes: Record<string, string>;
  video_urls?: Record<string, string>; purpose_sources: Partial<Record<VideoPurpose, string>>;
  requirements: string; output: VideoOutput; script: VideoScriptSegment[]; prompt: string;
  input_digest: string; model_signature: string; model: string; revision: number;
}
export interface VideoSubmission {
  id: string; request_id: string; job_id: string; state: string; remote_task_id?: string; result_url?: string;
  snapshot: VideoGenerationSnapshot; created_at: string;
}
export interface VideoRewriteResult {
  asset_id: string; preview_asset_id?: string; cover_asset_id?: string; job_id: string; submission_id: string;
  snapshot: VideoGenerationSnapshot; warning?: string; created_at: string;
}
export interface VideoRewriteState {
  script_rules_version?: number;
  initial_source_asset_id?: string;
  primary_asset_id: string; reference_video_id: string; reference_image_id: string;
  video_urls?: Record<string, string>; purpose_sources: Partial<Record<VideoPurpose, string>>;
  requirements: string; output: VideoOutput; script: VideoScriptSegment[]; prompt: string;
  script_input_digest: string; script_model_signature: string; initialization_complete: boolean;
  analyses: { asset_id: string; sha256: string; kind: string; model_signature: string; rules_version: number;
    features: Record<string, string>; transcript?: string }[];
  submissions: VideoSubmission[]; results: VideoRewriteResult[]; applied_version_id?: string;
}
export interface VideoCapabilities {
  video_configured: boolean; text_configured: boolean; vision_configured: boolean; asr_configured: boolean;
  provider?: string; models?: string[]; max_reference_video_duration?: number; max_reference_video_total?: number; max_total_video_duration?: number; max_video_mb?: number; max_image_mb?: number;
  supported: boolean; min_duration: number; max_duration: number; resolutions: string[]; ratios: string[]; rules_version: number;
}
export interface RewriteDetail { rewrite: Rewrite; note: Note; assets: Asset[]; videos: VideoProject[] }
export interface ImageFeatures { subject: string; action?: string; scene: string; composition: string; palette: string; lighting: string; texture: string; text: string }
export interface ImageFeatureControl { mode: 'image' | 'custom' | 'auto'; asset_id?: string; value?: string }
export type ImageFeatureControls = Partial<Record<keyof ImageFeatures, ImageFeatureControl>>;
export interface ImageProposal { id: string; title: string; summary: string; feature_controls: ImageFeatureControls }
export interface ImageAnalysis { asset_id: string; width: number; height: number; features: ImageFeatures; sha256?: string; recognized_text?: string; text_error?: string }
export interface ImageReference { asset_id: string; library_id?: string; title?: string }
export interface ImageOutput { ratio: string; clarity: string; count: number }
export interface ImageEditIntent { mode: 'flexible' | 'preserve_content' | 'feature_mix'; style?: string; palette?: string }
export interface ImageBatch {
  selected_proposal?: ImageProposal; extra_requirements?: string;
  id: string; request_id: string; revision: number; state: string; prompt: string; primary_asset_id: string;
  input_asset_ids: string[]; output: ImageOutput; model: string; created_at: string;
  feature_sources?: Partial<Record<keyof ImageFeatures, string>>; feature_controls?: ImageFeatureControls; prompt_rules_version?: number; edit_intent?: ImageEditIntent;
  items: { index: number; job_id?: string; state: string; asset_id?: string; error?: string }[];
}
export interface ImageCapabilities {
  image_configured: boolean; vision_configured: boolean; text_configured: boolean; supported: boolean; max_count: number;
  prompt_rules_version?: number;
  sizes: { ratio: string; clarity: string; width: number; height: number }[];
}
export interface ImageStyle { preset: string; custom: string; palette: string; composition: string; lighting: string; typography: string; ratio: string; retain: string[] }
export interface ImageResult { asset_id: string; revision: number; prompt: string; model: string; created_at: string; ratio?: string; source_asset_ids?: string[]; style_reference_id?: string;
  selected_proposal?: ImageProposal; extra_requirements?: string;
  batch_id?: string; primary_asset_id?: string; width?: number; height?: number; warning?: string; provider_request_id?: string;
  feature_sources?: Partial<Record<keyof ImageFeatures, string>>; feature_controls?: ImageFeatureControls; prompt_rules_version?: number; edit_intent?: ImageEditIntent }
export interface ImageRewrite {
  workflow_version?: number; primary_asset_id?: string; references?: ImageReference[]; feature_sources?: Partial<Record<keyof ImageFeatures, string>>; feature_controls?: ImageFeatureControls;
  proposals?: ImageProposal[]; proposals_input_digest?: string; selected_proposal_id?: string; extra_requirements?: string;
  output?: ImageOutput; edit_intent?: ImageEditIntent; text_mode?: string; prompt_input_digest?: string; prompt_rules_version?: number; batches?: ImageBatch[];
  id: string; version_id: string; applied_version_id?: string; source_asset_ids: string[]; style_reference_id?: string;
  style_library_id?: string; style_reference_title?: string;
  analyses: ImageAnalysis[]; style: ImageStyle; brief?: string; image_text?: string; prompt: string; revision: number; status: string; error?: string;
  results: ImageResult[]; created_at: string; updated_at: string;
}
export interface StyleReference { id: string; title: string; category: string; tags: string[]; credit: string }
export interface StyleCatalogue extends List<StyleReference> { categories: { id: string; name: string }[] }
export interface LibraryMedia {
  id: string; asset: Asset; rewrite_id: string; project_title: string; version_id: string;
  origin: 'image_rewrite' | 'video_rewrite' | 'video_image' | 'video_render' | 'upload'; revision: number; model?: string;
  cover_asset_id?: string; preview_asset_id?: string; created_at: string; stale: boolean;
}
export interface CollectionDetail { note: Note; assets: Asset[] }
export interface Publication {
  id: string; rewrite_id: string; version_id: string; title: string; body: string; topics: string[];
  asset_ids: string[]; video_project_id?: string; video_revision?: number; status: string;
  created_at: string; published_at?: string; published_url?: string; submitted_at?: string;
  publish_job_id?: string; publish_error?: string; publish_account_id?: string; publish_account_name?: string;
  publish_method?: string; xhs_note_id?: string; visibility?: XHSVisibility;
}
export interface XHSAccount { id: string; nickname: string; avatar?: string; red_id?: string }
export interface XHSAccountState {
  status: 'logged_out' | 'logged_in' | 'expired' | 'unavailable'; account?: XHSAccount;
  message?: string; dependency_ready: boolean;
}
export interface XHSLogin {
  login_id: string; status: 'initializing' | 'waiting_scan' | 'scanned' | 'logged_in' | 'expired' | 'failed';
  qr_image?: string; expires_at?: string; message?: string; account?: XHSAccount;
}
export type XHSVisibility = 'public' | 'private';
export type PublishAsset = Pick<Asset, 'id' | 'kind' | 'mime' | 'status' | 'position' | 'width' | 'height' | 'duration' | 'size'> & { sha256?: string };
export interface PublishPreview {
  confirmation_token: string; expires_at: string; account: XHSAccount; publication_id: string;
  title: string; body: string; topics: string[]; assets: PublishAsset[]; media_type: 'image' | 'video'; visibility: XHSVisibility;
  preview_image?: string;
}
export type Platform = 'xiaohongshu';
export interface ProviderConfig {
  base_url: string; model: string; api_key?: string; workspace_id?: string;
  configured: boolean; key_mask?: string;
}
export type Capability = 'text' | 'ocr' | 'asr' | 'video' | 'image' | 'vision';
export interface Settings {
  collector_url: string; cookie?: string; cookie_configured: boolean; cookie_mask?: string;
  providers: Record<Capability, ProviderConfig>;
  ffmpeg_path: string; ffprobe_path: string; node_path: string;
}
export interface Job {
  id: string; kind: string; entity_id: string; asset_id?: string; scene_id?: string; state: string;
  phase: string; progress: number; message?: string; error?: string; created_at: string;
  updated_at: string; attempts: number;
}
export interface AsyncResult { job_id?: string; job_ids?: string[]; rejected_items?: { url: string; error: string }[]; existing_ids?: string[] }
export interface Draft { title: string; body: string; topics: string[]; script: string; asset_ids: string[] }
