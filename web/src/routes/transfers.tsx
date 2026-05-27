import { createFileRoute } from '@tanstack/react-router'
import Transfers from '../pages/Transfers'

export const Route = createFileRoute('/transfers')({
  component: Transfers,
})
