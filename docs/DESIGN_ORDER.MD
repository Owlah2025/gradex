# DESIGN_ORDER.md

# Gradex Design Roadmap

> Status: Planning
> Last Updated: 2026-07-21

This document defines the order in which the Gradex MVP screens should be designed.

The goal is to establish navigation, layout patterns, components, and interaction models early so later screens reuse existing design decisions instead of inventing new ones.

---

# Phase 1 — Foundation (Core Product)

These screens establish the visual language and most reusable components.

## 1. Landing Page
Purpose:
- First impression
- Explain value proposition
- Drive registration
- Drive course discovery

Reusable Components:
- Header
- Footer
- Hero
- CTA
- Testimonials
- FAQ
- Course cards

---

## 2. Catalog

Purpose:
Browse courses.

Introduces:
- Search
- Filters
- Course grid
- Pagination

---

## 3. Course Details

Purpose:
Convert visitors into customers.

Introduces:
- Course overview
- Curriculum
- Instructor profile
- Reviews
- Purchase CTA

---

## 4. Login

Purpose:
Authentication.

---

## 5. Register

Purpose:
Account creation.

---

## 6. Checkout

Purpose:
Purchase flow.

Introduces:
- Order summary
- Coupon
- Payment
- Validation

---

## 7. Receipt / Payment Success

Purpose:
Purchase confirmation.

---

## 8. Student Dashboard

Purpose:
Learning hub.

Introduces:
- Continue Learning
- My Courses
- Recommendations

---

## 9. Course Home

Purpose:
Course overview after enrollment.

Introduces:
- Curriculum
- Progress
- Resources
- Community link

---

## 10. Lesson Player

Purpose:
Primary product experience.

Introduces:
- Video player
- Lesson navigation
- Progress tracking
- Downloads

---

# Phase 2 — Student Supporting Screens

## 11. Search Results

## 12. Lesson Resources & Labs

## 13. Notification Center

## 14. Profile

## 15. Account / Settings

## 16. Legal

## 17. Error States

---

# Phase 3 — Instructor Portal

Desktop-first.

## 18. Instructor Dashboard

## 19. Course Builder

## 20. Lesson Editor

## 21. Resources & Labs Manager

## 22. Submit for Review

## 23. Course Analytics

## 24. Payout Statements

---

# Phase 4 — Admin Portal

Desktop-first.

## 25. Admin Ops Landing

## 26. Moderation Queue

## 27. Content Review

## 28. User Management

## 29. Revenue Dashboard

## 30. Refunds

## 31. Payouts

## 32. Reported Content

---

# Phase 5 — Shared System Screens

These complete the MVP.

## 33. Forgot Password

## 34. Reset Password

---

# Design Rules

Never redesign existing patterns.

Every new screen must reuse:

- Navigation
- Cards
- Forms
- Buttons
- Tables
- Empty states
- Loading states
- Typography
- Spacing
- Colors

The design system is the single source of truth.

A screen is considered complete only after:

✓ UX Review
✓ Accessibility Review
✓ Responsive Design
✓ Component Audit
✓ Design Approval

Only then proceed to the next screen.