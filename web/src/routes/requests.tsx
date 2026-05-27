import { createFileRoute } from '@tanstack/react-router'
import Requests from '../pages/Requests'

export const Route = createFileRoute('/requests')({
  component: Requests,
})
