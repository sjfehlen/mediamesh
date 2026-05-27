import { createRootRoute, Link, Outlet } from '@tanstack/react-router'
import { TanStackRouterDevtools } from '@tanstack/router-devtools'

export const Route = createRootRoute({
  component: () => (
    <div className="min-h-screen bg-gray-50">
      <nav className="bg-white border-b px-6 py-3 flex items-center gap-6 text-sm font-medium">
        <span className="text-lg font-bold mr-4">MediaMesh</span>
        {[
          { to: '/', label: 'Library' },
          { to: '/requests', label: 'Requests' },
          { to: '/transfers', label: 'Transfers' },
          { to: '/peers', label: 'Peers' },
          { to: '/users', label: 'Users' },
          { to: '/audit', label: 'Audit' },
          { to: '/settings', label: 'Settings' },
        ].map(({ to, label }) => (
          <Link
            key={to}
            to={to}
            className="text-gray-600 hover:text-gray-900 [&.active]:text-blue-600 [&.active]:font-semibold"
          >
            {label}
          </Link>
        ))}
      </nav>
      <main className="container mx-auto max-w-6xl">
        <Outlet />
      </main>
      {import.meta.env.DEV && <TanStackRouterDevtools />}
    </div>
  ),
})
