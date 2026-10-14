package email

// The contact reminder is the one bilingual message Gradex sends (D-118). Every
// other template renders in the reader's own language; this one goes to every
// registered Student in one campaign, so it carries Arabic first and English
// second and each half has its own button into its own language.

// contactReminderURL is where a button lands: the Student's own profile, with
// the WhatsApp field brought into view. A signed-out Student is sent through
// sign-in by the profile page and returned here with the query intact. It
// carries no credential, so a forwarded message grants nothing.
func contactReminderURL(publicOrigin, locale string) string {
	return publicOrigin + "/" + locale + "/learn/profile?focus=contact"
}

// The outer document follows the Student's chosen language; each half still
// declares its own language and direction.
func (r *Renderer) renderContactReminder(request RenderRequest) (Message, error) {
	arabic := localizedTemplates[TemplateContactReminder]["ar"]
	english := localizedTemplates[TemplateContactReminder]["en"]
	arabicURL := contactReminderURL(r.publicOrigin, "ar")
	englishURL := contactReminderURL(r.publicOrigin, "en")
	htmlBody, err := renderBilingualHTML(request.Locale, arabic, english, arabicURL, englishURL, r.logoURL)
	if err != nil {
		return Message{}, err
	}
	text := renderText("ar", arabic, arabicURL, "", "") + "\n\n----------\n\n" + renderText("en", english, englishURL, "", "")
	return Message{
		From:      r.from,
		Recipient: request.Payload.Destination,
		ReplyTo:   r.replyTo,
		Subject:   arabic.Subject + " | " + english.Subject,
		Text:      text,
		HTML:      htmlBody,
	}, nil
}
