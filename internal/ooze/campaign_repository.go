package ooze

import (
	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/ooze/internal/campaign"
)

type campaignRepository struct{ Repository }

func (repository campaignRepository) MaterializeTemporaryRepository(path string) campaign.TemporaryRepository {
	return campaignTemporaryRepository{TemporaryRepository: repository.Repository.MaterializeTemporaryRepository(path)}
}

type campaignTemporaryRepository struct{ TemporaryRepository }

func (repository campaignTemporaryRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	return repository.TemporaryRepository.ListGoSourceFiles()
}

func (repository campaignTemporaryRepository) MaterializeTemporaryRepository(path string) campaign.TemporaryRepository {
	return campaignTemporaryRepository{TemporaryRepository: repository.TemporaryRepository.MaterializeTemporaryRepository(path)}
}
