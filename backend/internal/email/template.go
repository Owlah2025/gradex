package email

import (
	"bytes"
	"html/template"
)

var htmlMessageTemplate = template.Must(template.New("email").Parse(`<!doctype html>
<html lang="{{.Locale}}" dir="{{.Direction}}">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width,initial-scale=1">
    <meta name="color-scheme" content="light">
    <meta name="supported-color-schemes" content="light">
    <title>{{.Title}}</title>
    <style>
      @media screen and (max-width: 620px) {
        .email-gutter { padding: 20px 10px !important; }
        .email-content { padding: 28px 22px !important; }
        .email-heading { font-size: 25px !important; }
      }
    </style>
  </head>
  <body style="margin:0;padding:0;background:#f3f7fb;color:#0d1b2a;font-family:Arial,'Helvetica Neue',Helvetica,sans-serif;direction:{{.Direction}};text-align:{{.Align}};-webkit-text-size-adjust:100%;-ms-text-size-adjust:100%;">
    <div style="display:none!important;max-height:0;max-width:0;overflow:hidden;opacity:0;color:transparent;font-size:1px;line-height:1px;">
      {{.Preheader}}
    </div>
    <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" bgcolor="#f3f7fb" style="width:100%;border-collapse:collapse;background:#f3f7fb;">
      <tr>
        <td class="email-gutter" align="center" style="padding:36px 12px;">
          <table role="presentation" class="email-shell" width="600" cellpadding="0" cellspacing="0" border="0" bgcolor="#ffffff" style="width:100%;max-width:600px;border-collapse:separate;background:#ffffff;border:1px solid #dbe4ee;border-radius:12px;overflow:hidden;">
            <tr>
              <td style="padding:0;">
                <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="width:100%;border-collapse:collapse;">
                  <tr>
                    <td align="center" bgcolor="#0d1b2a" style="padding:24px 24px 20px;background:#0d1b2a;">
                      <img src="{{.LogoURL}}" alt="GradeX" width="180" height="84" style="display:block;width:180px;height:84px;max-width:100%;border:0;outline:none;text-decoration:none;color:#ffffff;font-family:Arial,'Helvetica Neue',Helvetica,sans-serif;font-size:18px;">
                    </td>
                  </tr>
                  <tr>
                    <td height="3" bgcolor="{{.AccentColor}}" style="height:3px;line-height:3px;font-size:1px;background:{{.AccentColor}};">&nbsp;</td>
                  </tr>
                </table>
              </td>
            </tr>
            <tr>
              <td class="email-content" align="{{.Align}}" bgcolor="#ffffff" style="padding:36px 42px 32px;background:#ffffff;text-align:{{.Align}};">
                <p style="margin:0 0 12px;color:{{.AccentColor}};font-family:Tahoma,Arial,'Segoe UI',sans-serif;font-size:12px;font-weight:bold;letter-spacing:1.6px;line-height:1.4;text-transform:uppercase;">{{.Context}}</p>
                <h1 class="email-heading" style="margin:0 0 18px;color:#0d1b2a;font-family:Arial,'Helvetica Neue',Helvetica,sans-serif;font-size:28px;font-weight:700;line-height:1.25;">{{.Title}}</h1>
                <p style="margin:0;color:#364453;font-family:Arial,'Helvetica Neue',Helvetica,sans-serif;font-size:16px;line-height:1.65;">{{.Body}}</p>
                {{if .Code}}
                <table role="presentation" dir="ltr" align="center" cellpadding="0" cellspacing="0" border="0" style="margin:28px auto 24px;border-collapse:collapse;direction:ltr;">
                  <tr>
                    <td align="center" bgcolor="#eef2ff" style="padding:15px 24px;background:#eef2ff;border:1px solid #a8c1ff;border-radius:8px;">
                      <span dir="ltr" style="display:block;color:#0d1b2a;font-family:'Courier New',Courier,monospace;font-size:32px;font-weight:bold;letter-spacing:8px;line-height:1.2;direction:ltr;unicode-bidi:isolate;">{{.Code}}</span>
                    </td>
                  </tr>
                </table>
                {{end}}
                {{if .Expiry}}
                <p style="margin:0 0 24px;color:#4c5a6b;font-family:Tahoma,Arial,'Segoe UI',sans-serif;font-size:14px;line-height:1.6;"><strong>{{.ExpiryLabel}}</strong> <span dir="ltr" style="direction:ltr;unicode-bidi:isolate;">{{.Expiry}}</span></p>
                {{end}}
                {{if .ActionURL}}
                <table role="presentation" dir="ltr" align="center" cellpadding="0" cellspacing="0" border="0" style="margin:26px auto 12px;border-collapse:collapse;direction:ltr;">
                  <tr>
                    <td align="center" bgcolor="#1e4ed8" style="background:#1e4ed8;border-radius:8px;">
                      <a href="{{.ActionURL}}" style="display:inline-block;padding:14px 24px;color:#ffffff;font-family:Arial,'Helvetica Neue',Helvetica,sans-serif;font-size:16px;font-weight:bold;line-height:1.2;text-decoration:none;">{{.Action}}</a>
                    </td>
                  </tr>
                </table>
                {{end}}
                <p style="margin:0;padding-top:20px;border-top:1px solid #e2e8f0;color:#4c5a6b;font-family:Tahoma,Arial,'Segoe UI',sans-serif;font-size:14px;line-height:1.65;">{{.Footer}}</p>
              </td>
            </tr>
            <tr>
              <td align="center" bgcolor="#f8fafc" style="padding:18px 24px;background:#f8fafc;color:#64748b;font-family:Arial,'Helvetica Neue',Helvetica,sans-serif;font-size:13px;line-height:1.5;text-align:center;">{{.FooterLabel}}</td>
            </tr>
          </table>
        </td>
      </tr>
    </table>
  </body>
</html>`))

func renderHTML(locale, templateContract string, copy localizedTemplate, actionURL, expiry, code, logoURL string) (string, error) {
	direction, align := "ltr", "left"
	if locale == "ar" {
		direction, align = "rtl", "right"
	}
	data := struct {
		Locale, Direction, Align, LogoURL, Preheader, Context, AccentColor             string
		Title, Body, Action, ActionURL, Footer, FooterLabel, Expiry, ExpiryLabel, Code string
	}{
		Locale:      locale,
		Direction:   direction,
		Align:       align,
		LogoURL:     logoURL,
		Preheader:   preheaderFor(locale, templateContract),
		Context:     contextLabelFor(locale, templateContract),
		AccentColor: accentColorFor(templateContract),
		Title:       copy.Title,
		Body:        copy.Body,
		Action:      copy.Action,
		ActionURL:   actionURL,
		Footer:      copy.Footer,
		FooterLabel: footerLabelFor(locale),
		Expiry:      expiry,
		ExpiryLabel: expiryLabelFor(locale, code != ""),
		Code:        code,
	}
	var buffer bytes.Buffer
	if err := htmlMessageTemplate.Execute(&buffer, data); err != nil {
		return "", err
	}
	return buffer.String(), nil
}

func contextLabelFor(locale, templateContract string) string {
	labels := map[string][2]string{
		TemplateVerifyEmail:      {"EMAIL VERIFICATION", "تأكيد البريد الإلكتروني"},
		TemplateVerifyEmailOTP:   {"EMAIL VERIFICATION", "تأكيد البريد الإلكتروني"},
		TemplateDeviceTrustOTP:   {"DEVICE SECURITY", "أمان الجهاز"},
		TemplatePasswordReset:    {"ACCOUNT SECURITY", "أمان الحساب"},
		TemplatePasswordChanged:  {"ACCOUNT SECURITY", "أمان الحساب"},
		TemplateStaffInvitation:  {"STAFF INVITATION", "دعوة فريق العمل"},
		TemplateCourseInvitation: {"COURSE ACCESS", "الوصول إلى الدورة"},
		TemplateAccessGranted:    {"COURSE ACCESS", "الوصول إلى الدورة"},
		TemplateBundleGranted:    {"COURSE ACCESS", "الوصول إلى الدورة"},
		TemplateInviteRejected:   {"COURSE ACCESS", "الوصول إلى الدورة"},
		TemplateInviteCancelled:  {"COURSE ACCESS", "الوصول إلى الدورة"},
		TemplateAccessAdjusted:   {"COURSE ACCESS", "الوصول إلى الدورة"},
		TemplateAccessRevoked:    {"COURSE ACCESS", "الوصول إلى الدورة"},
	}
	label := labels[templateContract]
	if locale == "ar" {
		return label[1]
	}
	return label[0]
}

func preheaderFor(locale, templateContract string) string {
	if locale == "ar" {
		if templateContract == TemplateVerifyEmailOTP {
			return "استخدم رمز التحقق لإكمال إعداد حسابك في Gradex."
		}
		return "رسالة آلية من Gradex تتعلق بأمان حسابك."
	}
	if templateContract == TemplateVerifyEmailOTP {
		return "Use this verification code to complete your Gradex account setup."
	}
	return "An automated Gradex message about your account security."
}

func footerLabelFor(locale string) string {
	if locale == "ar" {
		return "هذه رسالة آلية من Gradex."
	}
	return "This is an automated message from Gradex."
}

func accentColorFor(templateContract string) string {
	switch templateContract {
	case TemplateAccessGranted, TemplateBundleGranted, TemplatePasswordChanged, TemplateAccessAdjusted:
		return "#4f7cff"
	case TemplateInviteRejected, TemplateInviteCancelled, TemplateAccessRevoked:
		return "#64748b"
	default:
		return "#ff7e4d"
	}
}
