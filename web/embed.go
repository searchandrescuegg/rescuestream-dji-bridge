// Package web embeds the static assets served to operators — the JSBridge
// onboarding page loaded inside DJI Pilot 2's WebView.
package web

import _ "embed"

// OnboardingPage is the JSBridge onboarding HTML, an html/template whose fields
// (AppID, AppKey, License, PlatformName, WorkspaceID) are injected by the
// onboarding server (§5.1).
//
//go:embed onboarding/index.html
var OnboardingPage string

// VConsoleJS is the bundled vConsole debug console, served to the WebView so
// operators get on-device console/network/error visibility during onboarding.
//
//go:embed onboarding/vconsole.min.js
var VConsoleJS []byte
