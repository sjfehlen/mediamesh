import { createFileRoute } from '@tanstack/react-router'
import Peers from '../pages/Peers'

export const Route = createFileRoute('/peers')({
  component: Peers,
})
