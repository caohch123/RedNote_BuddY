package app

import (
	"errors"
	"strings"

	"xhs-workbench/internal/domain"
	"xhs-workbench/internal/providers"
)

const imageProposalRulesVersion = 1

func clearImageProposals(u *domain.ImageRewrite) {
	u.Proposals = nil
	u.ProposalsInputDigest = ""
	u.SelectedProposalID = ""
}

func selectedImageProposal(u domain.ImageRewrite) *domain.ImageProposal {
	for _, proposal := range u.Proposals {
		if proposal.ID == u.SelectedProposalID {
			copy := proposal
			copy.FeatureControls = copyImageFeatureControls(proposal.FeatureControls)
			return &copy
		}
	}
	return nil
}

// Recommendations consider every chosen image; eventual generation still
// sends only the images referenced by the user's selected proposal.
func (a *App) imageV2ProposalInput(rewriteID string, u domain.ImageRewrite, config map[string]domain.ProviderConfig) (providers.ImageProposalInput, string, error) {
	input := providers.ImageProposalInput{Ratio: u.Output.Ratio, Images: []providers.ImageProposalImage{}}
	hashes, err := a.imageV2Files(rewriteID, u)
	if err != nil {
		return input, "", err
	}
	for index, id := range imageV2IDs(u) {
		analysis, exists := imageV2Analysis(u, id)
		if !exists || analysis.SHA256 != hashes[index] || strings.TrimSpace(analysis.Features.Action) == "" {
			return input, "", errors.New("图片分析已过期，请重新分析图片")
		}
		features := map[string]string{}
		for _, name := range imageFlexibleFeatureNames {
			features[name] = featureValue(analysis.Features, name)
		}
		text := strings.TrimSpace(analysis.RecognizedText)
		if text == "" {
			var asset domain.Asset
			if a.Store.Get("assets", id, &asset) == nil {
				text = strings.TrimSpace(asset.Extraction.Text)
			}
		}
		if len([]rune(text)) > 4000 {
			return input, "", errors.New("图中文字过长，请精简识别文字后重新分析")
		}
		role := "reference"
		if index == 0 {
			role = "primary"
		}
		input.Images = append(input.Images, providers.ImageProposalImage{Number: index + 1, AssetID: id, Role: role, Features: features, RecognizedText: text})
	}
	if len(input.Images) == 0 {
		return input, "", errors.New("请选择被仿写图")
	}
	basis := input
	basis.Ratio = "" // Output dimensions are chosen again during prompt review.
	digest := digestImageValue([]any{basis, imageV2IDs(u), hashes, imageConfigSignature(config["vision"]), imageConfigSignature(config["text"]), imageProposalRulesVersion})
	return input, digest, nil
}

func validateImageProposals(u domain.ImageRewrite, proposals []domain.ImageProposal) error {
	if len(proposals) != 3 {
		return errors.New("分析须返回三个可选择的方案，请重试")
	}
	seen := map[string]bool{}
	for _, proposal := range proposals {
		if proposal.ID == "" || seen[proposal.ID] || strings.TrimSpace(proposal.Title) == "" || strings.TrimSpace(proposal.Summary) == "" {
			return errors.New("分析方案标识或说明无效，请重试")
		}
		seen[proposal.ID] = true
		candidate := u
		candidate.EditIntent = domain.ImageEditIntent{Mode: "flexible"}
		candidate.FeatureControls = proposal.FeatureControls
		if err := validateImageV2Controls(candidate, false); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) validateImageProposalSelection(rewriteID string, u domain.ImageRewrite) error {
	proposal := selectedImageProposal(u)
	if proposal == nil {
		return errors.New("请先从分析结果中选择一个方案")
	}
	settings, err := a.settings()
	if err != nil {
		return err
	}
	_, digest, err := a.imageV2ProposalInput(rewriteID, u, settings.Providers)
	if err != nil {
		return err
	}
	if digest != u.ProposalsInputDigest || digestImageValue(proposal.FeatureControls) != digestImageValue(u.FeatureControls) {
		return errors.New("图片或分析依据已变化，请重新分析并确认方案")
	}
	return nil
}
