import { createContext, useContext, useEffect, useState, ReactNode } from 'react'
import * as api from './api'

interface AuthState {
  user: api.User | null
  loading: boolean
  login: (email: string, password: string, twoFactorCode?: string) => Promise<{ requires_2fa?: boolean }>
  register: (email: string, password: string) => Promise<void>
  logout: () => void
}

const Ctx = createContext<AuthState>({
  user: null,
  loading: true,
  login: async () => ({ requires_2fa: false }),
  register: async () => {},
  logout: () => {},
})

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<api.User | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const token = localStorage.getItem('goexchange_token')
    if (token) {
      api.me().then(setUser).catch(() => {
        localStorage.removeItem('goexchange_token')
      }).finally(() => setLoading(false))
    } else {
      setLoading(false)
    }
  }, [])

  async function login(email: string, password: string, twoFactorCode?: string) {
    const res = await api.login(email, password)
    if (res.requires_2fa) {
      // 2FA required - need to verify code
      if (!twoFactorCode) {
        // Return indicator so Login page can show 2FA input
        return { requires_2fa: true }
      }
      // Complete 2FA login
      const result = await api.complete2FALogin(res.temp_token!, twoFactorCode)
      localStorage.setItem('goexchange_token', result.token)
      setUser(result.user)
      return { requires_2fa: false }
    }
    // Normal login (no 2FA)
    if (!res.token || !res.user) {
      throw new Error('invalid login response')
    }
    localStorage.setItem('goexchange_token', res.token)
    setUser(res.user)
    return { requires_2fa: false }
  }

  async function register(email: string, password: string) {
    const { user, token } = await api.register(email, password)
    localStorage.setItem('goexchange_token', token)
    setUser(user)
  }

  function logout() {
    localStorage.removeItem('goexchange_token')
    setUser(null)
  }

  return (
    <Ctx.Provider value={{ user, loading, login, register, logout }}>
      {children}
    </Ctx.Provider>
  )
}

export const useAuth = () => useContext(Ctx)