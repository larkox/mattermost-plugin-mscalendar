// Copyright (c) 2019-present Mattermost, Inc. All Rights Reserved.
// See License for license information.

package mscalendar

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	"golang.org/x/oauth2"

	"github.com/mattermost/mattermost-server/v5/model"

	"github.com/mattermost/mattermost-plugin-mscalendar/server/config"
	"github.com/mattermost/mattermost-plugin-mscalendar/server/store"
	"github.com/mattermost/mattermost-plugin-mscalendar/server/utils"
	"github.com/mattermost/mattermost-plugin-mscalendar/server/utils/oauth2connect"
)

const BotWelcomeMessage = "Bot user connected to account %s."

const RemoteUserAlreadyConnected = "%s account `%s` is already mapped to Mattermost account `%s`. Please run `/%s disconnect`, while logged in as the Mattermost account."
const RemoteUserAlreadyConnectedNotFound = "%s account `%s` is already mapped to a Mattermost account, but the Mattermost user could not be found."

type oauth2App struct {
	Env
}

func NewOAuth2App(env Env) oauth2connect.App {
	return &oauth2App{
		Env: env,
	}
}

func (app *oauth2App) InitOAuth2(mattermostUserID string) (url string, err error) {
	user, err := app.Store.LoadUser(mattermostUserID)
	if err == nil {
		return "", fmt.Errorf("User is already connected to %s", user.Remote.Mail)
	}

	conf := app.Remote.NewOAuth2Config()
	state := fmt.Sprintf("%v_%v", model.NewId()[0:15], mattermostUserID)
	err = app.Store.StoreOAuth2State(state)
	if err != nil {
		return "", err
	}

	return conf.AuthCodeURL(state, oauth2.AccessTypeOffline), nil
}

func (app *oauth2App) CompleteOAuth2(authedUserID, code, state string) error {
	if authedUserID == "" || code == "" || state == "" {
		return errors.New("missing user, code or state")
	}

	oconf := app.Remote.NewOAuth2Config()

	err := app.Store.VerifyOAuth2State(state)
	if err != nil {
		return errors.WithMessage(err, "missing stored state")
	}

	mattermostUserID := strings.Split(state, "_")[1]
	if mattermostUserID != authedUserID {
		return errors.New("not authorized, user ID mismatch")
	}

	ctx := context.Background()
	tok, err := oconf.Exchange(ctx, code)
	if err != nil {
		return err
	}

	client := app.Remote.MakeClient(ctx, tok)
	me, err := client.GetMe()
	if err != nil {
		return err
	}

	uid, err := app.Store.LoadMattermostUserID(me.ID)
	if err == nil {
		user, userErr := app.PluginAPI.GetMattermostUser(uid)
		if userErr == nil {
			app.Poster.DM(authedUserID, RemoteUserAlreadyConnected, config.ApplicationName, me.Mail, config.CommandTrigger, user.Username)
			return fmt.Errorf(RemoteUserAlreadyConnected, config.ApplicationName, me.Mail, config.CommandTrigger, user.Username)
		} else {
			// Couldn't fetch connected MM account. Reject connect attempt.
			app.Poster.DM(authedUserID, RemoteUserAlreadyConnectedNotFound, config.ApplicationName, me.Mail)
			return fmt.Errorf(RemoteUserAlreadyConnectedNotFound, config.ApplicationName, me.Mail)
		}
	}

	encryptedToken, err := encryptToken(tok, app.Config.TokenEncryptionKey)
	if err != nil {
		return err
	}

	u := &store.User{
		PluginVersion:    app.Config.PluginVersion,
		MattermostUserID: mattermostUserID,
		Remote:           me,
		OAuth2Token:      encryptedToken,
	}

	err = app.Store.StoreUser(u)
	if err != nil {
		return err
	}

	err = app.Store.StoreUserInIndex(u)
	if err != nil {
		return err
	}

	app.Welcomer.AfterSuccessfullyConnect(mattermostUserID, me.Mail)

	return nil
}

func decryptToken(tok *oauth2.Token, encryptionKey string) (*oauth2.Token, error) {
	unencryptedToken, err := utils.Decrypt([]byte(encryptionKey), tok.AccessToken)
	if err != nil {
		return nil, errors.New("cannot decrypt token")
	}

	newToken := &oauth2.Token{
		AccessToken:  unencryptedToken,
		TokenType:    tok.TokenType,
		RefreshToken: tok.RefreshToken,
	}
	return newToken, nil
}

func encryptToken(tok *oauth2.Token, encryptionKey string) (*oauth2.Token, error) {
	encryptedToken, err := utils.Encrypt([]byte(encryptionKey), tok.AccessToken)
	if err != nil {
		return nil, err
	}

	newToken := &oauth2.Token{
		AccessToken:  encryptedToken,
		TokenType:    tok.TokenType,
		RefreshToken: tok.RefreshToken,
	}
	return newToken, nil
}
