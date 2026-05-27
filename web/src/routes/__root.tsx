import { createRootRoute, Link, Outlet, useNavigate, useRouterState } from '@tanstack/react-router'
import { useEffect } from 'react'
import { api, clearToken, getToken } from '../api/client'

export const Route = createRootRoute({
  component: RootLayout,
})

function RootLayout() {
  const navigate = useNavigate()
  const { location } = useRouterState()
  const isLoginPage = location.pathname === '/login'

  useEffect(() => {
    if (!isLoginPage && !getToken()) {
      navigate({ to: '/login' })
    }
  }, [isLoginPage, navigate])

  if (isLoginPage) {
    return <Outlet />
  }

  return (
    <div className="min-h-screen bg-gray-50">
      <nav className="bg-white border-b px-6 py-3 flex items-center gap-6 text-sm font-medium">
        <span className="text-lg font-bold mr-4">MediaMesh</span>
        <div className="flex items-center gap-6 flex-1">
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
        </div>
        <button
          onClick={async () => {
            try { await api.post('/api/auth/logout') } catch { /* ignore */ }
            clearToken()
            navigate({ to: '/login' })
          }}
          className="text-gray-500 hover:text-gray-900 text-sm"
        >
          Sign out
        </button>
      </nav>
      <main className="container mx-auto max-w-6xl px-4 py-6">
        <Outlet />
      </main>
    </div>
  )
}
