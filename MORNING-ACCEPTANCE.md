# Morning Acceptance Guide

## How to start local stack
1. Ensure Docker is running.
2. In `backend/`, run `docker compose up -d` (requires docker compose plugin)
3. Start backend API: `cd backend && go run ./cmd/api`
4. Start backend Worker: `cd backend && go run ./cmd/worker`
5. Start frontend: `cd frontend && npm run dev`

## Local URLs
- **Student App**: http://localhost:3000/en/learn
- **Instructor App**: http://localhost:3000/en/instructor
- **Admin App**: http://localhost:3000/en/admin

## Local Disposable Accounts
- Admin: `admin@example.test` (password: `password`)
- Instructor: `instructor@example.test` (password: `password`)
- Student: `student@example.test` (password: `password`)

## Testing the Flow
### Student Flow
1. Login as Student
2. Check My Profile
3. Go to My Learning and Continue a course
4. Check Arabic RTL using the language toggle

### Instructor Flow
1. Login as Instructor
2. View Dashboard and Profile
3. Open Course Builder, create new course
4. Add sections, submit for review

### Admin Flow
1. Login as Admin
2. View Operator Dashboard
3. Open User 360, suspend/restore users
4. Check Audit logs
