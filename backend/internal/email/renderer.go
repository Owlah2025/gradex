package email

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

const (
	TemplateVerifyEmail      = "student-email-verification-v1"
	TemplateVerifyEmailOTP   = "student-email-verification-otp-v1"
	TemplatePasswordReset    = "account-password-reset-v1"
	TemplatePasswordChanged  = "account-password-reset-completed-v1"
	TemplateStaffInvitation  = "staff-invitation-v1"
	TemplateCourseInvitation = "course-access-invitation-v1"
	TemplateAccessGranted    = "course-access-granted-v1"
	TemplateInviteRejected   = "course-access-invitation-rejected-v1"
	TemplateInviteCancelled  = "course-access-invitation-cancelled-v1"
	TemplateAccessAdjusted   = "course-access-adjusted-v1"
	TemplateAccessRevoked    = "course-access-revoked-v1"
)

var eventTemplates = map[string]string{
	"identity.email_verification_requested":      TemplateVerifyEmail,
	"identity.email_verification_code_requested": TemplateVerifyEmailOTP,
	"identity.password_reset_requested":          TemplatePasswordReset,
	"identity.password_reset_completed":          TemplatePasswordChanged,
	"identity.staff_invitation_created":          TemplateStaffInvitation,
	"access.invitation_issued":                   TemplateCourseInvitation,
	"access.granted":                             TemplateAccessGranted,
	"access.invitation_rejected":                 TemplateInviteRejected,
	"access.invitation_cancelled":                TemplateInviteCancelled,
	"access.entitlement_adjusted":                TemplateAccessAdjusted,
	"access.entitlement_revoked":                 TemplateAccessRevoked,
}

// DeliveryPayload is the decrypted delivery instruction.
//
// VerificationToken is the credential slot: for a link template it is the
// bearer that goes in the URL, and for the OTP template it is the six-digit
// code that goes in the body. One field rather than two because the outbox
// ciphertext has one secret shape, and the template — not the payload — is
// what decides how that secret reaches the reader.
type DeliveryPayload struct {
	Destination       string    `json:"destination"`
	Locale            string    `json:"locale"`
	TemplateContract  string    `json:"template_contract"`
	VerificationToken string    `json:"verification_token"`
	ExpiresAt         time.Time `json:"expires_at"`
}

type RenderRequest struct {
	Event    outbox.Event
	Template string
	Locale   string
	Payload  DeliveryPayload
}

type RendererOptions struct {
	PublicOrigin string
	FromAddress  string
	FromName     string
	ReplyTo      string
}

type Renderer struct {
	publicOrigin string
	from         string
	replyTo      string
}

func NewRenderer(options RendererOptions) (*Renderer, error) {
	origin, err := url.Parse(options.PublicOrigin)
	if err != nil || origin.Scheme == "" || origin.Host == "" || origin.User != nil ||
		origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return nil, errors.New("transactional email public origin must be an absolute HTTP origin")
	}
	from := (&mail.Address{Name: strings.TrimSpace(options.FromName), Address: strings.TrimSpace(options.FromAddress)}).String()
	if _, err := mail.ParseAddress(from); err != nil || strings.TrimSpace(options.FromName) == "" {
		return nil, errors.New("transactional email sender is invalid")
	}
	replyTo := strings.TrimSpace(options.ReplyTo)
	if replyTo != "" {
		parsed, err := mail.ParseAddress(replyTo)
		if err != nil || parsed.Address != replyTo {
			return nil, errors.New("transactional email reply-to is invalid")
		}
	}
	return &Renderer{publicOrigin: options.PublicOrigin, from: from, replyTo: replyTo}, nil
}

type localizedTemplate struct {
	Subject string
	Title   string
	Body    string
	Action  string
	Footer  string
}

var localizedTemplates = map[string]map[string]localizedTemplate{
	TemplateVerifyEmail: {
		"en": {"Verify your Gradex email", "Verify your email address", "Use this link to verify your email address and finish activating your Gradex account.", "Verify email", "If you did not create this account, you can ignore this message."},
		"ar": {"تحقق من بريدك الإلكتروني في Gradex", "تحقق من عنوان بريدك الإلكتروني", "استخدم هذا الرابط للتحقق من بريدك الإلكتروني وإكمال تفعيل حسابك في Gradex.", "تحقق من البريد", "إذا لم تنشئ هذا الحساب، يمكنك تجاهل هذه الرسالة."},
	},
	// The OTP message carries no link and no action button on purpose. A code
	// the reader types into a page they already have open cannot be harvested
	// by a forwarded URL, and offering both a link and a code would put two
	// live credentials in one mailbox for one challenge.
	TemplateVerifyEmailOTP: {
		"en": {"Your Gradex verification code", "Verify your email address", "Enter this code on the Gradex verification screen to finish creating your account.", "", "If you did not create this account, you can ignore this message. Nobody from Gradex will ever ask you for this code."},
		"ar": {"رمز التحقق الخاص بك في Gradex", "تحقق من عنوان بريدك الإلكتروني", "أدخل هذا الرمز في شاشة التحقق في Gradex لإكمال إنشاء حسابك.", "", "إذا لم تنشئ هذا الحساب، يمكنك تجاهل هذه الرسالة. لن يطلب منك أحد من Gradex هذا الرمز أبدًا."},
	},
	TemplatePasswordReset: {
		"en": {"Reset your Gradex password", "Reset your password", "Use this link to choose a new Gradex password. The link can be used only once.", "Reset password", "If you did not request a reset, you can ignore this message."},
		"ar": {"إعادة تعيين كلمة مرور Gradex", "أعد تعيين كلمة المرور", "استخدم هذا الرابط لاختيار كلمة مرور جديدة في Gradex. يمكن استخدام الرابط مرة واحدة فقط.", "إعادة تعيين كلمة المرور", "إذا لم تطلب إعادة التعيين، يمكنك تجاهل هذه الرسالة."},
	},
	TemplatePasswordChanged: {
		"en": {"Your Gradex password was changed", "Password changed", "The password for your Gradex account was changed and existing sessions were ended.", "Sign in", "If this was not you, contact Gradex support immediately."},
		"ar": {"تم تغيير كلمة مرور Gradex", "تم تغيير كلمة المرور", "تم تغيير كلمة مرور حسابك في Gradex وإنهاء الجلسات الحالية.", "تسجيل الدخول", "إذا لم تقم بهذا التغيير، تواصل مع دعم Gradex فورًا."},
	},
	TemplateStaffInvitation: {
		"en": {"You are invited to join Gradex staff", "Complete your Gradex staff invitation", "Use this private link to review the assigned role and create your Gradex staff account.", "Complete invitation", "Do not forward this single-use invitation link."},
		"ar": {"دعوة للانضمام إلى فريق Gradex", "أكمل دعوة حساب فريق Gradex", "استخدم هذا الرابط الخاص لمراجعة الدور المعيّن وإنشاء حساب فريق Gradex.", "إكمال الدعوة", "لا تشارك رابط الدعوة المخصص للاستخدام مرة واحدة."},
	},
	TemplateCourseInvitation: {
		"en": {"Gradex Course Access Invitation", "A Course Access Invitation is available", "Accepting this invitation is only one step in the access process. Acceptance grants no Course access; an authorized Admin must approve it before access becomes active.", "Review invitation", "Do not forward this invitation link."},
		"ar": {"دعوة وصول إلى دورة في Gradex", "توجد دعوة وصول إلى دورة", "قبول هذه الدعوة خطوة واحدة فقط في عملية الوصول. القبول لا يمنح أي وصول إلى الدورة؛ يجب أن يعتمدها مسؤول مخوّل قبل تفعيل الوصول.", "مراجعة الدعوة", "لا تشارك رابط الدعوة."},
	},
	TemplateAccessGranted: {
		"en": {"Your Gradex Course access is active", "Course access granted", "An authorized Admin approved your Course Access Invitation. Your Course access is now active for the approved period.", "View access", "Your access remains subject to its recorded expiry and account status."},
		"ar": {"تم تفعيل وصولك إلى دورة Gradex", "تم منح الوصول إلى الدورة", "اعتمد مسؤول مخوّل دعوة الوصول إلى الدورة. أصبح وصولك إلى الدورة فعالًا للفترة المعتمدة.", "عرض الوصول", "يظل الوصول خاضعًا لتاريخ الانتهاء المسجل وحالة الحساب."},
	},
	TemplateInviteRejected: {
		"en": {"Your Gradex Course invitation was not approved", "Course invitation rejected", "An Admin reviewed the accepted Course Access Invitation and did not approve Course access. No Entitlement or Enrollment was created.", "View access status", "Contact Gradex support if you believe this needs review."},
		"ar": {"لم تتم الموافقة على دعوة دورة Gradex", "تم رفض دعوة الدورة", "راجع مسؤول دعوة الوصول المقبولة ولم يعتمد الوصول إلى الدورة. لم يتم إنشاء استحقاق أو تسجيل.", "عرض حالة الوصول", "تواصل مع دعم Gradex إذا كنت تعتقد أن الأمر يحتاج إلى مراجعة."},
	},
	TemplateInviteCancelled: {
		"en": {"Your Gradex Course invitation was cancelled", "Course invitation cancelled", "The Course Access Invitation is no longer available. It did not grant Course access.", "View access status", "Contact Gradex support if you need help."},
		"ar": {"تم إلغاء دعوة دورة Gradex", "تم إلغاء دعوة الدورة", "لم تعد دعوة الوصول إلى الدورة متاحة. لم تمنح الدعوة وصولًا إلى الدورة.", "عرض حالة الوصول", "تواصل مع دعم Gradex إذا احتجت إلى مساعدة."},
	},
	TemplateAccessAdjusted: {
		"en": {"Your Gradex Course access period changed", "Course access period updated", "An authorized Admin changed the end date of your Course access. Your current access period is shown on your access page.", "View access", "Contact Gradex support if you believe this needs review."},
		"ar": {"تم تغيير فترة وصولك إلى دورة Gradex", "تم تحديث فترة الوصول إلى الدورة", "غيّر مسؤول مخوّل تاريخ انتهاء وصولك إلى الدورة. تظهر فترة وصولك الحالية في صفحة الوصول.", "عرض الوصول", "تواصل مع دعم Gradex إذا كنت تعتقد أن الأمر يحتاج إلى مراجعة."},
	},
	TemplateAccessRevoked: {
		"en": {"Your Gradex Course access was ended", "Course access revoked", "An authorized Admin ended your access to this Course. Your enrollment record and learning progress are retained.", "View access status", "Contact Gradex support if you believe this needs review."},
		"ar": {"تم إنهاء وصولك إلى دورة Gradex", "تم إلغاء الوصول إلى الدورة", "أنهى مسؤول مخوّل وصولك إلى هذه الدورة. يظل سجل تسجيلك وتقدمك الدراسي محفوظًا.", "عرض حالة الوصول", "تواصل مع دعم Gradex إذا كنت تعتقد أن الأمر يحتاج إلى مراجعة."},
	},
}

func (r *Renderer) Render(request RenderRequest) (Message, error) {
	copy, err := validateRenderRequest(request)
	if err != nil {
		return Message{}, err
	}
	if request.Template == TemplateCourseInvitation && purchaseBackedInvitation(request.Event) {
		copy = purchaseInvitationCopy(request.Locale)
	}
	actionURL, needsExpiry, err := r.actionURL(request)
	if err != nil {
		return Message{}, err
	}
	expiry := ""
	if needsExpiry {
		expiry, err = renderExpiringAction(request)
		if err != nil {
			return Message{}, err
		}
	}
	// The verification code is a rendered value, not a destination. Passing it
	// through the same slot as the action URL would eventually put it in an
	// href, which is exactly what the OTP flow exists to avoid.
	code := ""
	if request.Template == TemplateVerifyEmailOTP {
		code = request.Payload.VerificationToken
	}
	htmlBody, err := renderHTML(request.Locale, copy, actionURL, expiry, code)
	if err != nil {
		return Message{}, errors.New("transactional email HTML rendering failed")
	}
	return Message{From: r.from, Recipient: request.Payload.Destination, ReplyTo: r.replyTo, Subject: copy.Subject, Text: renderText(request.Locale, copy, actionURL, expiry, code), HTML: htmlBody}, nil
}

func purchaseBackedInvitation(event outbox.Event) bool {
	payload, ok := event.SafePayload.(map[string]any)
	if !ok {
		return false
	}
	purchaseBacked, _ := payload["purchase_backed"].(bool)
	return purchaseBacked
}

func purchaseInvitationCopy(locale string) localizedTemplate {
	if locale == "ar" {
		return localizedTemplate{
			Subject: "دعوة وصول مدفوعة إلى دورة Gradex",
			Title:   "دعوتك إلى الدورة جاهزة",
			Body:    "تم تأكيد الدفع الخارجي من Gradex. سجّل أو سجّل دخولك بالبريد الإلكتروني المقصود ثم اقبل الدعوة؛ عندها يصبح الوصول إلى الدورة نشطاً مباشرة.",
			Action:  "فتح الدعوة",
			Footer:  "لا تشارك رابط الدعوة المخصص للاستخدام مرة واحدة.",
		}
	}
	return localizedTemplate{
		Subject: "Your paid Gradex Course invitation",
		Title:   "Your Course invitation is ready",
		Body:    "Gradex has confirmed the external payment. Register or sign in with the intended email, then accept this invitation; Course access becomes active immediately.",
		Action:  "Open invitation",
		Footer:  "Do not forward this single-use invitation link.",
	}
}

func validateRenderRequest(request RenderRequest) (localizedTemplate, error) {
	expected, ok := eventTemplates[request.Event.Type]
	if !ok || expected != request.Template || request.Payload.TemplateContract != request.Template {
		return localizedTemplate{}, errors.New("transactional email event/template contract is unsupported")
	}
	if request.Locale != "ar" && request.Locale != "en" || request.Payload.Locale != request.Locale {
		return localizedTemplate{}, errors.New("transactional email locale is unsupported")
	}
	if strings.TrimSpace(request.Payload.Destination) == "" {
		return localizedTemplate{}, errors.New("transactional email destination is missing")
	}
	copy, ok := localizedTemplates[request.Template][request.Locale]
	if !ok {
		return localizedTemplate{}, errors.New("transactional email template is unavailable")
	}
	return copy, nil
}

func renderExpiringAction(request RenderRequest) (string, error) {
	if request.Payload.ExpiresAt.IsZero() {
		return "", errors.New("transactional email expiry is missing")
	}
	return request.Payload.ExpiresAt.UTC().Format("2006-01-02 15:04 UTC"), nil
}

// expiryLabelFor says what actually expires. A message with a code and a label
// reading "Link expires" tells the reader to look for a link that is not there.
func expiryLabelFor(locale string, hasCode bool) string {
	if locale == "ar" {
		if hasCode {
			return "تنتهي صلاحية الرمز:"
		}
		return "تنتهي صلاحية الرابط:"
	}
	if hasCode {
		return "Code expires:"
	}
	return "Link expires:"
}

func renderText(locale string, copy localizedTemplate, actionURL, expiry, code string) string {
	text := copy.Title + "\n\n" + copy.Body
	if code != "" {
		text += "\n\n" + code
	}
	if expiry != "" {
		text += "\n\n" + expiryLabelFor(locale, code != "") + " " + expiry
	}
	if actionURL != "" {
		text += "\n\n" + copy.Action + ": " + actionURL
	}
	text += "\n\n" + copy.Footer
	return text
}

func (r *Renderer) actionURL(request RenderRequest) (string, bool, error) {
	token := request.Payload.VerificationToken
	credential := url.QueryEscape(token)
	switch request.Template {
	case TemplateVerifyEmail:
		if token == "" {
			return "", false, errors.New("verification credential is missing")
		}
		return r.publicOrigin + "/verify-email/result#token=" + credential, true, nil
	case TemplateVerifyEmailOTP:
		// No URL at all. The code is typed into the screen the Student already
		// has open, so this message is not a navigation surface and carries
		// nothing clickable that could be forwarded or phished.
		if token == "" {
			return "", false, errors.New("verification code is missing")
		}
		return "", true, nil
	case TemplatePasswordReset:
		if token == "" {
			return "", false, errors.New("reset credential is missing")
		}
		return r.publicOrigin + "/recover/reset#token=" + credential, true, nil
	case TemplateStaffInvitation:
		if token == "" {
			return "", false, errors.New("staff invitation credential is missing")
		}
		return r.publicOrigin + "/staff/accept#token=" + credential, true, nil
	case TemplateCourseInvitation:
		if token == "" {
			return "", false, errors.New("course invitation credential is missing")
		}
		if request.Event.AggregateID == "" {
			return "", false, errors.New("course invitation identity is missing")
		}
		return fmt.Sprintf("%s/%s/access?invitation_id=%s#token=%s", r.publicOrigin, request.Locale, url.QueryEscape(request.Event.AggregateID), credential), true, nil
	case TemplatePasswordChanged:
		return r.publicOrigin + "/login", false, nil
	case TemplateAccessGranted, TemplateInviteRejected, TemplateInviteCancelled,
		TemplateAccessAdjusted, TemplateAccessRevoked:
		return r.publicOrigin + "/" + request.Locale + "/access", false, nil
	default:
		return "", false, errors.New("transactional email template is unsupported")
	}
}

var htmlMessageTemplate = template.Must(template.New("email").Parse(`<!doctype html>
<html lang="{{.Locale}}" dir="{{.Direction}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head>
<body style="margin:0;background:#f6f5f1;color:#17211b;font-family:Arial,sans-serif;direction:{{.Direction}};text-align:{{.Align}}">
<main style="max-width:600px;margin:0 auto;padding:24px"><div style="background:#ffffff;border:1px solid #dedbd2;border-radius:12px;padding:28px">
<h1 style="font-size:24px;line-height:1.3;margin:0 0 16px">{{.Title}}</h1><p style="font-size:16px;line-height:1.7">{{.Body}}</p>
{{if .Code}}<p style="margin:24px 0"><span style="display:inline-block;background:#f2f4f3;border:1px solid #dedbd2;border-radius:8px;padding:14px 22px;font-family:'Courier New',monospace;font-size:32px;font-weight:bold;letter-spacing:8px;direction:ltr;unicode-bidi:isolate">{{.Code}}</span></p>{{end}}
{{if .Expiry}}<p style="font-size:14px;line-height:1.6"><strong>{{.ExpiryLabel}}</strong> {{.Expiry}}</p>{{end}}
{{if .ActionURL}}<p style="margin:24px 0"><a href="{{.ActionURL}}" style="display:inline-block;background:#175c3a;color:#fff;text-decoration:none;padding:12px 18px;border-radius:8px">{{.Action}}</a></p>{{end}}
<p style="font-size:14px;line-height:1.6;color:#4f5b54">{{.Footer}}</p></div></main></body></html>`))

func renderHTML(locale string, copy localizedTemplate, actionURL, expiry, code string) (string, error) {
	direction, align := "ltr", "left"
	if locale == "ar" {
		direction, align = "rtl", "right"
	}
	expiryLabel := expiryLabelFor(locale, code != "")
	data := struct {
		Locale, Direction, Align, Title, Body, Action, ActionURL, Footer, Expiry, ExpiryLabel, Code string
	}{
		locale, direction, align, copy.Title, copy.Body, copy.Action, actionURL, copy.Footer, expiry, expiryLabel, code,
	}
	var buffer bytes.Buffer
	if err := htmlMessageTemplate.Execute(&buffer, data); err != nil {
		return "", err
	}
	return buffer.String(), nil
}
