package deploy

import (
	"context"

	"github.com/KangminNa/naru/internal/contract"
	"github.com/KangminNa/naru/internal/model"
)

const (
	keepImages   = 3 // 되돌리기용으로 남겨 둘 빌드 이미지 수 (지금 것 포함)
	keepReleases = 5 // 정적 사이트 배포본
)

type oldVersionCleaner struct {
	past   contract.DeployHistoryReader
	images contract.ImageCleaner
	files  contract.SiteFiles
}

// NewOldVersionCleaner는 되돌리기용으로 최근 버전만 남기고 오래된 빌드 이미지·정적 배포본을 지운다.
// 쓰는 중인 이미지는 Docker가 지우지 않는다. 지금 서빙 중인 배포본은 언제나 남는다.
func NewOldVersionCleaner(past contract.DeployHistoryReader, images contract.ImageCleaner, files contract.SiteFiles) contract.OldVersionCleaner {
	return oldVersionCleaner{past, images, files}
}

func (c oldVersionCleaner) Clean(ctx context.Context, s model.Service, live model.DeploymentID) {
	if s.Live.Release != "" {
		c.files.Prune(s.Name, keepReleases, live)
		return
	}
	if s.Live.Instance == "" {
		return
	}
	ids, err := c.past.Succeeded(ctx, s.ID)
	if err != nil {
		return
	}
	keep := map[model.DeploymentID]bool{live: true}
	for _, id := range ids {
		if len(keep) >= keepImages {
			break
		}
		keep[id] = true
	}
	for _, id := range ids {
		if !keep[id] {
			c.images.Remove(ctx, model.ImageTag(s.Name, id))
		}
	}
}
