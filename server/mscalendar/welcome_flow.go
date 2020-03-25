package mscalendar

import (
	"github.com/mattermost/mattermost-plugin-mscalendar/server/store"
	"github.com/mattermost/mattermost-plugin-mscalendar/server/utils/bot"
	"github.com/mattermost/mattermost-plugin-mscalendar/server/utils/flow"
)

type welcomeFlow struct {
	steps      []flow.Step
	url        string
	flower     bot.FlowController
	onFlowDone func(userID string)
}

func NewWelcomeFlow(url string, bot bot.FlowController, onFlowDone func(userID string)) *welcomeFlow {
	wf := welcomeFlow{
		url:        url,
		flower:     bot,
		onFlowDone: onFlowDone,
	}
	wf.makeSteps()
	return &wf
}

func (wf *welcomeFlow) Step(i int) flow.Step {
	if i < 0 {
		return nil
	}
	if i >= len(wf.steps) {
		return nil
	}
	return wf.steps[i]
}

func (wf *welcomeFlow) URL() string {
	return wf.url
}

func (wf *welcomeFlow) Length() int {
	return len(wf.steps)
}

func (wf *welcomeFlow) StepDone(userID string, value bool) {
	wf.flower.NextStep(userID, value)
}

func (wf *welcomeFlow) FlowDone(userID string) {
	wf.onFlowDone(userID)
}

func (wf *welcomeFlow) makeSteps() {
	steps := []flow.Step{}
	steps = append(steps, &flow.SimpleStep{
		Title:                "Update Status",
		Message:              "Do you wish your Mattermost status to be automatically updated to be *Do not disturb* at the time of your Microsoft Calendar events?",
		PropertyName:         store.UpdateStatusPropertyName,
		TrueButtonMessage:    "Yes - Update my status",
		FalseButtonMessage:   "No - Don't update my status",
		TrueResponseMessage:  ":thumbsup: Got it! We'll automatically update your status in Mattermost.",
		FalseResponseMessage: ":thumbsup: Got it! We won't update your status in Mattermost.",
		FalseSkip:            1,
	}, &flow.SimpleStep{
		Title:                "Confirm status change",
		Message:              "Do you want to receive confirmations before we update your status for each event?",
		PropertyName:         store.GetConfirmationPropertyName,
		TrueButtonMessage:    "Yes - I will like to get confirmations",
		FalseButtonMessage:   "No - Update my status automatically",
		TrueResponseMessage:  "Cool, we'll also send you confirmations before updating your status.",
		FalseResponseMessage: "Cool, we'll update your status automatically with no confirmation.",
	}, &flow.SimpleStep{
		Title:                "Subscribe to events",
		Message:              "Do you want to receive notifications when you receive a new event?",
		PropertyName:         store.SubscribePropertyName,
		TrueButtonMessage:    "Yes - I will like to receive notifications",
		FalseButtonMessage:   "No - Do not notify me of new events",
		TrueResponseMessage:  "Great, you will receive a message any time you receive a new event.",
		FalseResponseMessage: "Great, you will not receive any notification on new events.",
	})

	wf.steps = steps
}
