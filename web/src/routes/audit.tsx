import { createFileRoute } from '@tanstack/react-router'
import AuditLog from '../pages/AuditLog'

export const Route = createFileRoute('/audit')({
  component: AuditLog,
})
