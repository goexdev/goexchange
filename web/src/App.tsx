import { Routes, Route, Navigate } from 'react-router-dom'
import { NotFound } from './pages/NotFound'
import { useTranslation } from 'react-i18next'
import { Header } from './components/Header'
import { useAuth } from './lib/auth'
import { Home } from './pages/Home'
import { Login } from './pages/Login'
import { Register } from './pages/Register'
import { Markets } from './pages/Markets'
	import { Status } from "./pages/Status"
import { Trade } from './pages/Trade'
import { AuditLog } from './pages/AuditLog'
import { AdminChains } from './pages/AdminChains'
import { AdminPairs } from './pages/AdminPairs'
import { AdminUsers } from './pages/AdminUsers'
import { AdminDashboardPage } from './pages/AdminDashboard'
import { AdminKYC } from './pages/AdminKYC'
import { AdminCurrencies } from './pages/AdminCurrencies'
import { AdminFeeStats } from './pages/AdminFeeStats'
import { Notifications } from './pages/Notifications'
import { StatusBar } from './components/StatusBar'
import { User } from './pages/User'

function RequireAuth({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation()
  const { user, loading } = useAuth()
  if (loading) return <div className="p-8">{t('common.loadingEllipsis')}</div>
  if (!user) return <Navigate to="/login" replace />
  return <>{children}</>
}

export default function App() {
  return (
    <div className="min-h-screen flex flex-col">
      <Header />
      <main className="flex-1 container mx-auto px-4 py-6">
        <Routes>
          <Route path="/" element={<Home />} />
          <Route path="/login" element={<Login />} />
          <Route path="/register" element={<Register />} />
          <Route path="/markets" element={<Markets />} />
		<Route path="/status" element={<Status />} />
          <Route path="/trade/:base/:quote" element={<Trade />} />
          <Route
            path="/user"
            element={
              <RequireAuth>
                <User />
              </RequireAuth>
            }
          />
          {/* Legacy routes - redirect to /user?tab=... */}
          <Route path="/wallet" element={<Navigate to="/user?tab=wallets" replace />} />
          <Route path="/orders" element={<Navigate to="/user?tab=orders" replace />} />
          <Route path="/withdraw" element={<Navigate to="/user?tab=withdraw" replace />} />
          <Route path="/notifications" element={<Navigate to="/user?tab=notifications" replace />} />
          <Route path="/kyc" element={<Navigate to="/user?tab=kyc" replace />} />
          <Route path="/api-keys" element={<Navigate to="/user?tab=api-keys" replace />} />
          <Route path="/settings" element={<Navigate to="/user?tab=settings" replace />} />
          {/* Admin */}
          <Route
            path="/admin"
            element={
              <RequireAuth>
                <AdminDashboardPage />
              </RequireAuth>
            }
          />
          <Route
            path="/admin/chains"
            element={
              <RequireAuth>
                <AdminChains />
              </RequireAuth>
            }
          />
          <Route
            path="/admin/pairs"
            element={
              <RequireAuth>
                <AdminPairs />
              </RequireAuth>
            }
          />
          <Route
            path="/admin/kyc"
            element={
              <RequireAuth>
                <AdminKYC />
              </RequireAuth>
            }
          />
          <Route
            path="/admin/currencies"
            element={
              <RequireAuth>
                <AdminCurrencies />
              </RequireAuth>
            }
          />
          <Route
            path="/admin/fee-stats"
            element={
              <RequireAuth>
                <AdminFeeStats />
              </RequireAuth>
            }
          />
          <Route
            path="/admin/users"
            element={
              <RequireAuth>
                <AdminUsers />
              </RequireAuth>
            }
          />
          <Route
            path="/admin/audit"
            element={
              <RequireAuth>
                <AuditLog />
              </RequireAuth>
            }
          />
          <Route
            path="/admin/dashboard"
            element={
              <RequireAuth>
                <AdminDashboardPage />
              </RequireAuth>
            }
          />
          <Route path="*" element={<NotFound />} />
        </Routes>
      </main>
      <StatusBar />
    </div>
  )
}
