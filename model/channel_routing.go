package model

import "github.com/QuantumNous/new-api/setting/ratio_setting"

type ChannelAbilityCandidate struct {
	Ability Ability
	Channel *Channel
}

func GetEnabledChannelAbilityCandidates(group string, modelName string) ([]ChannelAbilityCandidate, error) {
	query := DB.Model(&Ability{}).Where("model = ? and enabled = ?", modelName, true)
	if group != "" {
		query = query.Where(&Ability{Group: group})
	}
	var abilities []Ability
	if err := query.Find(&abilities).Error; err != nil {
		return nil, err
	}
	if len(abilities) == 0 {
		normalizedModel := ratio_setting.FormatMatchingModelName(modelName)
		if normalizedModel != "" && normalizedModel != modelName {
			query = DB.Model(&Ability{}).Where("model = ? and enabled = ?", normalizedModel, true)
			if group != "" {
				query = query.Where(&Ability{Group: group})
			}
			if err := query.Find(&abilities).Error; err != nil {
				return nil, err
			}
		}
	}
	if len(abilities) == 0 {
		return []ChannelAbilityCandidate{}, nil
	}

	channelIDs := make([]int, 0, len(abilities))
	for _, ability := range abilities {
		channelIDs = append(channelIDs, ability.ChannelId)
	}
	var channels []*Channel
	if err := DB.Where("id IN ?", channelIDs).Find(&channels).Error; err != nil {
		return nil, err
	}
	channelByID := make(map[int]*Channel, len(channels))
	for _, channel := range channels {
		channelByID[channel.Id] = channel
	}

	candidates := make([]ChannelAbilityCandidate, 0, len(abilities))
	for _, ability := range abilities {
		candidates = append(candidates, ChannelAbilityCandidate{
			Ability: ability,
			Channel: channelByID[ability.ChannelId],
		})
	}
	return candidates, nil
}
