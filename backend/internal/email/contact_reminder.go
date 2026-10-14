package email

// The contact reminder (D-118) is an Arabic-only, high-priority message sent to
// every registered Student in one operator campaign. Its button opens the
// Student's own profile with the WhatsApp field brought into view; a signed-out
// Student is taken through sign-in and returned there. The link carries no
// credential, so a forwarded copy grants nothing.

func contactReminderURL(publicOrigin, locale string) string {
	return publicOrigin + "/" + locale + "/learn/profile?focus=contact"
}

func (r *Renderer) renderContactReminder(request RenderRequest) (Message, error) {
	copy := localizedTemplates[TemplateContactReminder]["ar"]
	actionURL := contactReminderURL(r.publicOrigin, "ar")
	htmlBody, err := renderUrgentArabicHTML(copy, actionURL, r.logoURL)
	if err != nil {
		return Message{}, err
	}
	return Message{
		From:         r.from,
		Recipient:    request.Payload.Destination,
		ReplyTo:      r.replyTo,
		Subject:      copy.Subject,
		Text:         "مطلوب\n\n" + renderText("ar", copy, actionURL, "", ""),
		HTML:         htmlBody,
		HighPriority: true,
	}, nil
}
