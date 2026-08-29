package processruntime

import (
	"slices"
	"time"
)

type Projection struct {
	capacity    int
	nextID      uint64
	mode        admissionMode
	lifecycle   runtimeLifecycle
	fatalCauses []runtimeFatalCause
	fatalEpoch  fatalEpochID
	fatalOwner  campaignToken
	campaigns   []registeredCampaign
	admissions  []imageAdmission
}

type imageAdmission struct {
	authority   imageAuthority
	stage       admissionStage
	generation  attemptGeneration
	overlapped  bool
	disposition admissionDisposition
}

type imageAuthority struct {
	campaign campaignToken
	attempt  attemptIdentity
	class    admissionClass
	profile  Profile
	deadline time.Duration
}

func (image Projection) Capacity() int { return image.capacity }

func (image Projection) Open() bool { return image.lifecycle == runtimeOpen }

func (image Projection) Closing() bool { return image.lifecycle == runtimeFatalClosing }

func (image Projection) Drained() bool { return image.lifecycle == runtimeClosedDrained }

func (image Projection) Unconfirmed() bool { return image.lifecycle == runtimeClosedUnconfirmed }

func (image Projection) SingleAdmission() bool { return image.mode == singleAdmission }

func (image Projection) FatalEpoch() uint64 { return uint64(image.fatalEpoch) }

func (image Projection) FatalCauseCount() int { return len(image.fatalCauses) }

func (image Projection) CampaignCount() int { return len(image.campaigns) }

func (image Projection) AdmissionCount() int { return len(image.admissions) }

func (image Projection) Owned(generation Generation) bool {
	index := image.admissionIndex(attemptGeneration(generation))
	return index >= 0 && image.admissions[index].stage == admissionOwned
}

func (image Projection) HasOverlappedPair() bool {
	count := 0
	for _, admission := range image.admissions {
		if admission.overlapped {
			count++
		}
	}
	return count >= 2
}

func (image Projection) Residual() []Residual {
	result := make([]Residual, 0, len(image.admissions))
	for _, admission := range image.admissions {
		if admission.stage != admissionProspective && admission.stage != admissionOwned {
			continue
		}
		result = append(result, Residual{
			generation: Generation(admission.generation), attempt: string(admission.authority.attempt),
			prospective: admission.stage == admissionProspective,
			transferred: admission.disposition == dispositionCustodyTransferred ||
				admission.disposition == dispositionCustodySettled,
		})
	}
	return result
}

func (image Projection) admissionIndex(generation attemptGeneration) int {
	for index, admission := range image.admissions {
		if generation != 0 && admission.generation == generation {
			return index
		}
	}
	return -1
}

func projectState(state processRuntime) Projection {
	image := Projection{
		capacity: state.capacity, nextID: state.nextID, mode: state.mode, lifecycle: state.lifecycle,
		fatalCauses: slices.Clone(state.fatalCauses), fatalEpoch: state.fatalEpoch, fatalOwner: state.fatalOwner,
		campaigns: slices.Clone(state.campaigns), admissions: make([]imageAdmission, len(state.admissions)),
	}
	for index, admission := range state.admissions {
		image.admissions[index] = imageAdmission{
			authority: imageAuthority{
				campaign: admission.grant.campaign, attempt: admission.grant.attempt,
				class: admission.grant.class, profile: admission.grant.profile, deadline: admission.grant.deadline,
			},
			stage: admission.stage, generation: admission.generation,
			overlapped: admission.overlapped, disposition: admission.disposition,
		}
	}
	return image
}
