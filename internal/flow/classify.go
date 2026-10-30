package flow

import "strings"

// known is a short list of hosts whose role is widely documented. It is a
// label to help reading, never a verdict: an unlisted host is simply unlisted.
var known = []struct {
	suffix   string
	category string
	owner    string
}{
	{"google-analytics.com", "analytics", "Google"},
	{"analytics.google.com", "analytics", "Google"},
	{"googletagmanager.com", "tag manager", "Google"},
	{"doubleclick.net", "advertising", "Google"},
	{"googlesyndication.com", "advertising", "Google"},
	{"googleadservices.com", "advertising", "Google"},
	{"gstatic.com", "cdn", "Google"},
	{"googleapis.com", "api", "Google"},
	{"facebook.com", "social", "Meta"},
	{"facebook.net", "analytics", "Meta"},
	{"fbcdn.net", "cdn", "Meta"},
	{"instagram.com", "social", "Meta"},
	{"segment.io", "analytics", "Twilio Segment"},
	{"segment.com", "analytics", "Twilio Segment"},
	{"mixpanel.com", "analytics", "Mixpanel"},
	{"amplitude.com", "analytics", "Amplitude"},
	{"hotjar.com", "session replay", "Hotjar"},
	{"fullstory.com", "session replay", "FullStory"},
	{"sentry.io", "error reporting", "Sentry"},
	{"bugsnag.com", "error reporting", "Bugsnag"},
	{"datadoghq.com", "monitoring", "Datadog"},
	{"newrelic.com", "monitoring", "New Relic"},
	{"nr-data.net", "monitoring", "New Relic"},
	{"intercom.io", "support chat", "Intercom"},
	{"hubspot.com", "marketing", "HubSpot"},
	{"hs-scripts.com", "marketing", "HubSpot"},
	{"cloudfront.net", "cdn", "Amazon"},
	{"amazonaws.com", "cloud", "Amazon"},
	{"akamaihd.net", "cdn", "Akamai"},
	{"akamaized.net", "cdn", "Akamai"},
	{"cdnjs.cloudflare.com", "cdn", "Cloudflare"},
	{"cloudflare.com", "cdn", "Cloudflare"},
	{"cloudflareinsights.com", "analytics", "Cloudflare"},
	{"fastly.net", "cdn", "Fastly"},
	{"jsdelivr.net", "cdn", "jsDelivr"},
	{"unpkg.com", "cdn", "unpkg"},
	{"stripe.com", "payments", "Stripe"},
	{"paypal.com", "payments", "PayPal"},
	{"apple.com", "platform", "Apple"},
	{"icloud.com", "platform", "Apple"},
	{"mzstatic.com", "cdn", "Apple"},
	{"microsoft.com", "platform", "Microsoft"},
	{"live.com", "platform", "Microsoft"},
	{"office.com", "platform", "Microsoft"},
	{"windows.com", "platform", "Microsoft"},
	{"clarity.ms", "session replay", "Microsoft"},
	{"bing.com", "search", "Microsoft"},
	{"github.com", "developer", "GitHub"},
	{"githubusercontent.com", "developer", "GitHub"},
	{"npmjs.org", "developer", "npm"},
	{"slack.com", "messaging", "Slack"},
	{"zoom.us", "meetings", "Zoom"},
	{"twitter.com", "social", "X"},
	{"x.com", "social", "X"},
	{"t.co", "links", "X"},
	{"tiktok.com", "social", "TikTok"},
	{"linkedin.com", "social", "LinkedIn"},
	{"licdn.com", "cdn", "LinkedIn"},
	{"criteo.com", "advertising", "Criteo"},
	{"adnxs.com", "advertising", "Xandr"},
	{"rubiconproject.com", "advertising", "Magnite"},
	{"pubmatic.com", "advertising", "PubMatic"},
	{"openx.net", "advertising", "OpenX"},
	{"taboola.com", "advertising", "Taboola"},
	{"outbrain.com", "advertising", "Outbrain"},
	{"scorecardresearch.com", "measurement", "Comscore"},
	{"quantserve.com", "measurement", "Quantcast"},
	{"onetrust.com", "consent", "OneTrust"},
	{"cookielaw.org", "consent", "OneTrust"},
	{"cookiebot.com", "consent", "Cookiebot"},
	{"osano.com", "consent", "Osano"},
	{"recaptcha.net", "bot check", "Google"},
	{"hcaptcha.com", "bot check", "hCaptcha"},
	{"launchdarkly.com", "feature flags", "LaunchDarkly"},
	{"optimizely.com", "experiments", "Optimizely"},
	{"braze.com", "messaging", "Braze"},
	{"onesignal.com", "push", "OneSignal"},
	{"firebaseio.com", "backend", "Google Firebase"},
	{"crashlytics.com", "error reporting", "Google Firebase"},
	{"app-measurement.com", "analytics", "Google Firebase"},
	{"anthropic.com", "ai", "Anthropic"},
	{"openai.com", "ai", "OpenAI"},
}

// Classify returns a category and owner for a host from the known list, or
// empty strings when the host is not listed.
func Classify(host string) (category, owner string) {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	for _, k := range known {
		if h == k.suffix || strings.HasSuffix(h, "."+k.suffix) {
			return k.category, k.owner
		}
	}
	return "", ""
}
