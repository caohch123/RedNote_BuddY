package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"xhs-workbench/internal/domain"
)

func seedSelectedVideoPublication(t *testing.T, withCover bool) (apiFixture, *fakeXHSRPC, domain.Publication) {
	t.Helper()
	f, rpc, original := seedXHSFixture(t)
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.app.Data, "assets", "publish-video.mp4")
	if output, err := exec.Command(ffmpeg, "-nostdin", "-y", "-f", "lavfi", "-i", "color=c=blue:s=320x240:r=12", "-t", "1", "-c:v", "libx264", "-threads", "1", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-an", path).CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	video := domain.Asset{ID: "selected-video", OwnerID: original.RewriteID, Kind: "video", MIME: "video/mp4", Status: "ready", LocalPath: "assets/publish-video.mp4", SHA256: digestVideoBytes(data), Size: int64(len(data)), Duration: 1, Width: 320, Height: 240}
	if err := f.app.Store.Put("assets", video.ID, video); err != nil {
		t.Fatal(err)
	}
	var rewrite domain.Rewrite
	if err := f.app.Store.Get("rewrites", original.RewriteID, &rewrite); err != nil {
		t.Fatal(err)
	}
	rewrite.Versions[0].AssetIDs = []string{video.ID}
	if withCover {
		rewrite.Versions[0].AssetIDs = append(rewrite.Versions[0].AssetIDs, original.AssetIDs[0])
	}
	if err := f.app.Store.Put("rewrites", rewrite.ID, rewrite); err != nil {
		t.Fatal(err)
	}
	p := decodeResponse[domain.Publication](t, f.call(t, "POST", "/publications?platform=xiaohongshu", map[string]any{"rewrite_id": rewrite.ID, "version_id": rewrite.Versions[0].ID, "asset_ids": rewrite.Versions[0].AssetIDs}, true), 201)
	return f, rpc, p
}

func TestXHSPublishSelectedVideoWithAutomaticOrSelectedCover(t *testing.T) {
	for _, withCover := range []bool{false, true} {
		t.Run(map[bool]string{false: "automatic_cover", true: "selected_cover"}[withCover], func(t *testing.T) {
			f, rpc, publication := seedSelectedVideoPublication(t, withCover)
			preview := previewXHSTest(t, f, publication)
			if preview.MediaType != "video" || len(preview.Assets) != 2 || preview.Assets[0].Kind != "video" || preview.Assets[1].Kind != "cover" || preview.Assets[0].Duration <= 0 {
				t.Fatalf("video preview invalid: %+v", preview)
			}
			var saved domain.Publication
			var rewrite domain.Rewrite
			if err := f.app.Store.Get("publications", publication.ID, &saved); err != nil {
				t.Fatal(err)
			}
			if err := f.app.Store.Get("rewrites", publication.RewriteID, &rewrite); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(rewrite.Versions[0].AssetIDs, publication.AssetIDs) {
				t.Fatal("preview changed the confirmed version")
			}
			if len(saved.AssetIDs) != 2 {
				t.Fatal("cover was not saved with the publication")
			}
			var cover domain.Asset
			if err := f.app.Store.Get("assets", preview.Assets[1].ID, &cover); err != nil {
				t.Fatal(err)
			}
			if withCover && cover.Kind != "image" {
				t.Fatal("selected image was modified")
			}
			if !withCover && (cover.Kind != "cover" || cover.OwnerID != publication.ID) {
				t.Fatal("automatic cover has incorrect ownership")
			}
			again := previewXHSTest(t, f, publication)
			if xhsFingerprint(again) != xhsFingerprint(preview) || rpc.count("publish") != 0 {
				t.Fatal("preview is unstable or submitted a publication")
			}
			var sent struct {
				Publication struct {
					MediaType string `json:"media_type"`
					Assets    []struct {
						Kind string `json:"kind"`
					} `json:"assets"`
				} `json:"publication"`
			}
			rpc.fn = func(method string, params any) (any, error) {
				if method != "publish" {
					return nil, nil
				}
				data, _ := json.Marshal(params)
				_ = json.Unmarshal(data, &sent)
				return map[string]any{"note_id": "507f1f77bcf86cd799439011", "visibility": "private", "account": rpc.profile}, nil
			}
			jobID := submitXHSTest(t, f, publication, again.ConfirmationToken)
			var job domain.Job
			if err := f.app.Store.Get("jobs", jobID, &job); err != nil {
				t.Fatal(err)
			}
			job.State = "running"
			f.app.runJob(&job)
			if job.State != "succeeded" || rpc.count("publish") != 1 || sent.Publication.MediaType != "video" || len(sent.Publication.Assets) != 2 || sent.Publication.Assets[1].Kind != "cover" {
				t.Fatalf("video did not reach the publisher once: job=%+v payload=%+v", job, sent)
			}
		})
	}
}

func TestXHSPublishSelectedVideoRejectsExtraMediaAndChangedFiles(t *testing.T) {
	f, rpc, publication := seedSelectedVideoPublication(t, false)
	var rewrite domain.Rewrite
	if err := f.app.Store.Get("rewrites", publication.RewriteID, &rewrite); err != nil {
		t.Fatal(err)
	}
	var video domain.Asset
	if err := f.app.Store.Get("assets", publication.AssetIDs[0], &video); err != nil {
		t.Fatal(err)
	}
	otherVideo := video
	otherVideo.ID = "second-video"
	if err := f.app.Store.Put("assets", otherVideo.ID, otherVideo); err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]string{{video.ID, otherVideo.ID}, {video.ID, "asset-fixture", "second-image"}} {
		var image domain.Asset
		if err := f.app.Store.Get("assets", "asset-fixture", &image); err != nil {
			t.Fatal(err)
		}
		image.ID = "second-image"
		if err := f.app.Store.Put("assets", image.ID, image); err != nil {
			t.Fatal(err)
		}
		rewrite.Versions[0].AssetIDs = ids
		publication.AssetIDs = ids
		if err := f.app.Store.Put("rewrites", rewrite.ID, rewrite); err != nil {
			t.Fatal(err)
		}
		if err := f.app.Store.Put("publications", publication.ID, publication); err != nil {
			t.Fatal(err)
		}
		response := f.call(t, "POST", "/publications/"+publication.ID+"/publish-preview", map[string]string{"visibility": "private"}, true)
		if response.Code != 409 {
			t.Fatalf("extra media accepted: %d %s", response.Code, response.Body.String())
		}
	}
	rewrite.Versions[0].AssetIDs = []string{video.ID}
	publication.AssetIDs = []string{video.ID}
	if err := f.app.Store.Put("rewrites", rewrite.ID, rewrite); err != nil {
		t.Fatal(err)
	}
	if err := f.app.Store.Put("publications", publication.ID, publication); err != nil {
		t.Fatal(err)
	}
	preview := previewXHSTest(t, f, publication)
	path := filepath.Join(f.app.Data, video.LocalPath)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, 'x'), 0600); err != nil {
		t.Fatal(err)
	}
	response := f.call(t, "POST", "/publications/"+publication.ID+"/publish", map[string]string{"confirmation_token": preview.ConfirmationToken}, true)
	if response.Code != 409 || rpc.count("publish") != 0 {
		t.Fatal("changed video was submitted")
	}
}
