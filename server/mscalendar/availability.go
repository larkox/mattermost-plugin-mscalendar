// Copyright (c) 2019-present Mattermost, Inc. All Rights Reserved.
// See License for license information.

package mscalendar

import (
	"fmt"
	"time"

	"github.com/mattermost/mattermost-plugin-mscalendar/server/remote"
	"github.com/mattermost/mattermost-plugin-mscalendar/server/store"
	"github.com/mattermost/mattermost-plugin-mscalendar/server/utils"
)

const (
	availabilityTimeWindowSize      = 15
	upcomingEventNotificationTime   = 10 * time.Minute
	upcomingEventNotificationWindow = (JOB_INTERVAL * 9) / 10 //90% of the Interval
)

type Availability interface {
	GetAvailabilities(remoteUserID string, scheduleIDs []string) ([]*remote.ScheduleInformation, error)
	SyncStatus(mattermostUserID string) (string, error)
	SyncStatusAll() (string, error)
}

func (m *mscalendar) SyncStatus(mattermostUserID string) (string, error) {
	return m.syncStatusUsers([]string{mattermostUserID})
}

func (m *mscalendar) SyncStatusAll() (string, error) {
	userIndex, err := m.Store.LoadUserIndex()
	if err != nil {
		if err.Error() == "not found" {
			return "No users found in user index", nil
		}
		return "", err
	}

	allIDs := userIndex.GetMattermostUserIDs()
	filteredIDs := []string{}
	for _, id := range allIDs {
		if id != m.Config.BotUserID {
			filteredIDs = append(filteredIDs, id)
		}
	}
	return m.syncStatusUsers(filteredIDs)
}

func (m *mscalendar) syncStatusUsers(mattermostUserIDs []string) (string, error) {
	err := m.Filter(
		withClient,
		withUserExpanded(m.actingUser),
	)
	if err != nil {
		return "", err
	}

	fullUserIndex, err := m.Store.LoadUserIndex()
	if err != nil {
		if err.Error() == "not found" {
			return "No users found in user index", nil
		}
		return "", err
	}

	filteredUsers := store.UserIndex{}
	indexByMattermostUserID := fullUserIndex.ByMattermostID()

	for _, mattermostUserID := range mattermostUserIDs {
		if u, ok := indexByMattermostUserID[mattermostUserID]; ok {
			filteredUsers = append(filteredUsers, u)
		}
	}

	if len(filteredUsers) == 0 {
		return "No connected users found", nil
	}

	scheduleIDs := []string{}
	for _, u := range filteredUsers {
		scheduleIDs = append(scheduleIDs, u.Email)
	}

	schedules, err := m.GetAvailabilities(m.actingUser.Remote.ID, scheduleIDs)
	if err != nil {
		return "", err
	}
	if len(schedules) == 0 {
		return "No schedule info found", nil
	}

	return m.setUserStatuses(filteredUsers, schedules, mattermostUserIDs)
}

func (m *mscalendar) setUserStatuses(filteredUsers store.UserIndex, schedules []*remote.ScheduleInformation, mattermostUserIDs []string) (string, error) {
	statuses, appErr := m.PluginAPI.GetMattermostUserStatusesByIds(mattermostUserIDs)
	if appErr != nil {
		return "", appErr
	}
	statusMap := map[string]string{}
	for _, s := range statuses {
		statusMap[s.UserId] = s.Status
	}

	usersByEmail := filteredUsers.ByEmail()
	var res string
	for _, s := range schedules {
		if s.Error != nil {
			m.Logger.Errorf("Error getting availability for %s: %s", s.ScheduleID, s.Error.ResponseCode)
			continue
		}

		mattermostUserID := usersByEmail[s.ScheduleID].MattermostUserID

		m.notifyUpcomingEvent(mattermostUserID, s.ScheduleItems)
		status, ok := statusMap[mattermostUserID]
		if !ok {
			continue
		}

		res = m.setStatusFromAvailability(mattermostUserID, status, s.AvailabilityView)
	}
	if res != "" {
		return res, nil
	}

	return utils.JSONBlock(schedules), nil
}

func (m *mscalendar) notifyUpcomingEvent(mattermostUserID string, items []remote.ScheduleItem) {
	for _, scheduleItem := range items {
		loc, err := time.LoadLocation(scheduleItem.Start.Timezone)
		if err != nil {
			m.Logger.Errorf("problem loading location: %s", err.Error())
			continue
		}
		now := time.Now().In(loc)
		start, err := time.Parse("2006-01-02T15:04:05MST", scheduleItem.Start.DateTime+scheduleItem.Start.Timezone)
		if err != nil {
			m.Logger.Errorf("problem parsing date: %s", err.Error())
			continue
		}
		if now.Add(upcomingEventNotificationTime).After(start.Add(-upcomingEventNotificationWindow)) && now.Add(upcomingEventNotificationTime).Before(start.Add(upcomingEventNotificationWindow)) {
			m.renderScheduleItem(mattermostUserID, scheduleItem)
		}
	}
}

func (m *mscalendar) renderScheduleItem(mattermostUserId string, s remote.ScheduleItem) {
	message := "You have an upcoming event:"
	start, err := time.Parse("2006-01-02T15:04:05MST", s.Start.DateTime+s.Start.Timezone)
	if err != nil {
		m.Logger.Errorf("problem parsing start date: %s", err.Error())
		return
	}

	end, err := time.Parse("2006-01-02T15:04:05MST", s.End.DateTime+s.End.Timezone)

	message = fmt.Sprintf("\n%s-%s", start.Format("15:04MST"), end.Format("15:04MST"))
	if s.Subject == "" {
		message += fmt.Sprintf("Subject: Not available. Check your privacy settings so we can show you the subject.")
	} else {
		message += fmt.Sprintf("\nSubject: %s", s.Subject)
	}

	if s.Location != "" {
		message += fmt.Sprintf("\nLocation: %s", s.Location)
	}

	m.Poster.DM(mattermostUserId, message)
}

func (m *mscalendar) GetAvailabilities(remoteUserID string, scheduleIDs []string) ([]*remote.ScheduleInformation, error) {
	client, err := m.MakeSuperuserClient()
	if err != nil {
		return nil, err
	}

	start := remote.NewDateTime(time.Now().UTC(), "UTC")
	end := remote.NewDateTime(time.Now().UTC().Add(availabilityTimeWindowSize*time.Minute), "UTC")

	return client.GetSchedule(remoteUserID, scheduleIDs, start, end, availabilityTimeWindowSize)
}

func (m *mscalendar) setStatusFromAvailability(mattermostUserID, currentStatus string, av remote.AvailabilityView) string {
	currentAvailability := av[0]

	switch currentAvailability {
	case remote.AvailabilityViewFree:
		if currentStatus == "dnd" {
			m.PluginAPI.UpdateMattermostUserStatus(mattermostUserID, "online")
			return fmt.Sprintf("User is free. Setting user from %s to online.", currentStatus)
		} else {
			return fmt.Sprintf("User is free, and is already set to %s.", currentStatus)
		}
	case remote.AvailabilityViewTentative, remote.AvailabilityViewBusy:
		if currentStatus != "dnd" {
			m.PluginAPI.UpdateMattermostUserStatus(mattermostUserID, "dnd")
			return fmt.Sprintf("User is busy. Setting user from %s to dnd.", currentStatus)
		} else {
			return fmt.Sprintf("User is busy, and is already set to %s.", currentStatus)
		}
	case remote.AvailabilityViewOutOfOffice:
		if currentStatus != "offline" {
			m.PluginAPI.UpdateMattermostUserStatus(mattermostUserID, "offline")
			return fmt.Sprintf("User is out of office. Setting user from %s to offline", currentStatus)
		} else {
			return fmt.Sprintf("User is out of office, and is already set to %s.", currentStatus)
		}
	case remote.AvailabilityViewWorkingElsewhere:
		return fmt.Sprintf("User is working elsewhere. Pending implementation.")
	}

	return fmt.Sprintf("Availability view doesn't match %d", currentAvailability)
}
