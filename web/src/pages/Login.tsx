import { useState } from 'react'
import { Eye, EyeOff, Lock, Mail, ArrowRight } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { api } from '../api/client'

// Ancient Egypt decorative components
const AnkhIcon = () => (
  <svg className="w-6 h-6 text-[#D4AF37]" viewBox="0 0 24 24" fill="currentColor">
    <path d="M8 3C8 1.9 8.9 1 10 1C11.1 1 12 1.9 12 3V8H16C17.1 8 18 8.9 18 10C18 11.1 17.1 12 16 12H12V21C12 22.1 11.1 23 10 23C8.9 23 8 22.1 8 21V12H4C2.9 12 2 11.1 2 10C2 8.9 2.9 8 4 8H8V3Z"/>
  </svg>
)

const ScarabDecoration = () => (
  <svg className="w-8 h-8 text-[#D4AF37] opacity-60" viewBox="0 0 24 24" fill="currentColor">
    <path d="M12 2C10 2 8.5 3.5 8.5 5.5C8.5 6.5 9 7.5 10 8.2V9H7C5.5 9 4.5 10 4.5 11.5C4.5 13 5.5 14 7 14H10V15H6C4.5 15 3.5 16 3.5 17.5C3.5 19 4.5 20 6 20H10V21C10 22.1 10.9 23 12 23C13.1 23 14 22.1 14 21V20H18C19.5 20 20.5 19 20.5 17.5C20.5 16 19.5 15 18 15H14V14H17C18.5 14 19.5 13 19.5 11.5C19.5 10 18.5 9 17 9H14V8.2C15 7.5 15.5 6.5 15.5 5.5C15.5 3.5 14 2 12 2Z"/>
  </svg>
)

const EyeOfHorusIcon = () => (
  <svg className="w-6 h-6 text-[#40E0D0]" viewBox="0 0 24 24" fill="currentColor">
    <path d="M12 4.5C7 4.5 2.7 7.6 1 12C2.7 16.4 7 19.5 12 19.5C17 19.5 21.3 16.4 23 12C21.3 7.6 17 4.5 12 4.5ZM12 17C8.1 17 4.8 14.9 3.2 12C4.8 9.1 8.1 7 12 7C15.9 7 19.2 9.1 20.8 12C19.2 14.9 15.9 17 12 17ZM12 9C10.3 9 9 10.3 9 12C9 13.7 10.3 15 12 15C13.7 15 15 13.7 15 12C15 10.3 13.7 9 12 9Z"/>
  </svg>
)

const LotusIcon = () => (
  <svg className="w-5 h-5 text-[#40E0D0] opacity-50" viewBox="0 0 24 24" fill="currentColor">
    <path d="M12 22C12 22 20 16 20 10C20 6 17 3 12 3C7 3 4 6 4 10C4 16 12 22 12 22ZM12 5C15 5 17 7 17 10C17 14 12 18 12 18C12 18 7 14 7 10C7 7 9 5 12 5Z"/>
  </svg>
)

export function Login() {
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoading(true)
    setError('')

    try {
      const result = await api.post<{ user: { id: string; email: string; name: string }; token: string }>('/auth/login', {
        email,
        password,
      })
      api.setToken(result.token)
      navigate('/')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'The gods have rejected your offering')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-gradient-to-br from-[#0a0a15] via-[#0f172a] to-[#0a0a15] flex items-center justify-center p-4 relative overflow-hidden">
      {/* Ancient Egypt Background Effects */}
      <div className="fixed inset-0 overflow-hidden pointer-events-none">
        {/* Pyramid shadows */}
        <div className="absolute bottom-0 left-0 w-0 h-0 border-l-[300px] border-l-transparent border-r-[300px] border-r-transparent border-b-[500px] border-b-[#0f172a]/30" />
        <div className="absolute bottom-0 right-0 w-0 h-0 border-l-[200px] border-l-transparent border-r-[200px] border-r-transparent border-b-[350px] border-b-[#0f172a]/20" />

        {/* Golden glow orbs */}
        <div className="absolute top-1/4 left-1/4 w-96 h-96 bg-[#D4AF37]/5 rounded-full blur-3xl animate-pulse" />
        <div className="absolute bottom-1/4 right-1/4 w-96 h-96 bg-[#40E0D0]/5 rounded-full blur-3xl" />

        {/* Hieroglyphic pattern overlay */}
        <div className="absolute inset-0 hieroglyph-pattern opacity-30" />
      </div>

      <div className="w-full max-w-md relative z-10">
        {/* Logo Section - Ancient Egypt themed */}
        <div className="text-center mb-8">
          <div className="relative inline-flex items-center justify-center mb-6">
            {/* Decorative ring */}
            <div className="absolute inset-0 bg-[#D4AF37]/20 rounded-full blur-xl animate-pulse" />
            <div className="relative w-24 h-24 rounded-full bg-gradient-to-br from-[#1a1a2e] to-[#0a0a15] flex items-center justify-center
                          border-2 border-[#D4AF37] shadow-lg shadow-[#D4AF37]/30 glow-gold">
              <img src="/jackal-logo.svg" alt="Anubis" className="w-16 h-16" />
            </div>

            {/* Decorative elements around logo */}
            <div className="absolute -top-2 left-1/2 -translate-x-1/2">
              <AnkhIcon />
            </div>
            <div className="absolute -bottom-1 left-1/2 -translate-x-1/2">
              <ScarabDecoration />
            </div>
          </div>

          <h1 className="text-4xl font-cinzel font-bold gradient-gold-shine mb-2 tracking-wider">Anubis</h1>
          <p className="text-lg font-cormorant text-[#D4AF37]/60 italic mb-1">The Judgment Never Sleeps</p>
          <div className="flex items-center justify-center gap-2">
            <div className="h-px w-12 bg-gradient-to-r from-transparent to-[#D4AF37]/40" />
            <span className="text-xs text-gray-500 font-cinzel tracking-[0.2em]">HALL OF MA'AT</span>
            <div className="h-px w-12 bg-gradient-to-l from-transparent to-[#D4AF37]/40" />
          </div>
        </div>

        {/* Login Card - Egyptian themed */}
        <div className="relative bg-gradient-to-br from-gray-900/90 to-[#0a0a15]/90 border border-[#D4AF37]/30 rounded-2xl p-8 shadow-2xl backdrop-blur-sm">
          {/* Decorative top border */}
          <div className="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-transparent via-[#D4AF37] to-transparent" />

          {/* Corner decorations */}
          <div className="absolute top-4 left-4 w-6 h-6 border-t-2 border-l-2 border-[#D4AF37]/30 rounded-tl-lg" />
          <div className="absolute top-4 right-4 w-6 h-6 border-t-2 border-r-2 border-[#D4AF37]/30 rounded-tr-lg" />
          <div className="absolute bottom-4 left-4 w-6 h-6 border-b-2 border-l-2 border-[#D4AF37]/30 rounded-bl-lg" />
          <div className="absolute bottom-4 right-4 w-6 h-6 border-b-2 border-r-2 border-[#D4AF37]/30 rounded-br-lg" />

          {error && (
            <div className="mb-6 p-4 bg-rose-500/10 border border-rose-500/20 rounded-xl">
              <p className="text-sm text-rose-400 flex items-center gap-2 font-cormorant">
                <EyeOfHorusIcon />
                {error}
              </p>
            </div>
          )}

          <form onSubmit={handleSubmit} className="space-y-6">
            {/* Email Field */}
            <div>
              <label className="block text-sm font-cinzel text-[#D4AF37]/80 mb-2 tracking-wide">
                Sacred Email
              </label>
              <div className="relative group">
                <Mail className="absolute left-4 top-1/2 -translate-y-1/2 w-5 h-5 text-[#D4AF37]/50 group-focus-within:text-[#D4AF37] transition-colors" />
                <input
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder="priest@anubis.watch"
                  className="w-full bg-gray-950/80 border border-[#D4AF37]/20 rounded-xl pl-12 pr-4 py-3 text-white placeholder:text-gray-600
                           focus:outline-none focus:border-[#D4AF37]/50 focus:ring-1 focus:ring-[#D4AF37]/30 transition-all font-cormorant text-lg"
                  required
                />
                {/* Decorative corner */}
                <div className="absolute top-0 right-0 w-3 h-3 border-t border-r border-[#D4AF37]/20 rounded-tr-xl opacity-0 group-focus-within:opacity-100 transition-opacity" />
              </div>
            </div>

            {/* Password Field */}
            <div>
              <label className="block text-sm font-cinzel text-[#D4AF37]/80 mb-2 tracking-wide">
                Secret Words
              </label>
              <div className="relative group">
                <Lock className="absolute left-4 top-1/2 -translate-y-1/2 w-5 h-5 text-[#D4AF37]/50 group-focus-within:text-[#D4AF37] transition-colors" />
                <input
                  type={showPassword ? 'text' : 'password'}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="••••••••"
                  className="w-full bg-gray-950/80 border border-[#D4AF37]/20 rounded-xl pl-12 pr-12 py-3 text-white placeholder:text-gray-600
                           focus:outline-none focus:border-[#D4AF37]/50 focus:ring-1 focus:ring-[#D4AF37]/30 transition-all font-cormorant text-lg"
                  required
                />
                <button
                  type="button"
                  onClick={() => setShowPassword(!showPassword)}
                  className="absolute right-4 top-1/2 -translate-y-1/2 text-gray-500 hover:text-[#D4AF37] transition-colors"
                >
                  {showPassword ? <EyeOff className="w-5 h-5" /> : <Eye className="w-5 h-5" />}
                </button>
              </div>
            </div>

            {/* Submit Button - Egyptian themed */}
            <button
              type="submit"
              disabled={loading}
              className="w-full flex items-center justify-center gap-2 bg-gradient-to-r from-[#B8860B] via-[#D4AF37] to-[#B8860B] hover:from-[#D4AF37] hover:via-[#F4D03F] hover:to-[#D4AF37]
                       text-gray-950 font-cinzel font-bold py-3 rounded-xl transition-all shadow-lg shadow-[#D4AF37]/30 disabled:opacity-50 disabled:cursor-not-allowed
                       border border-[#D4AF37] tracking-wider uppercase"
            >
              {loading ? (
                <div className="w-5 h-5 border-2 border-gray-900/30 border-t-gray-900 rounded-full animate-spin" />
              ) : (
                <>
                  Enter the Temple
                  <ArrowRight className="w-5 h-5" />
                </>
              )}
            </button>
          </form>

          {/* Decorative divider */}
          <div className="mt-6 pt-6 border-t border-[#D4AF37]/20">
            <div className="flex items-center justify-center gap-3">
              <LotusIcon />
              <p className="text-xs text-gray-500 font-cormorant text-center italic">
                Demo offering: <span className="text-[#D4AF37]">admin@anubis.watch</span> / <span className="text-[#D4AF37]">admin</span>
              </p>
              <LotusIcon />
            </div>
          </div>
        </div>

        {/* Footer - Ancient Egypt themed */}
        <div className="text-center mt-8">
          <p className="text-gray-500 text-sm font-cormorant italic">
            "Protected by the eternal judgment of Anubis"
          </p>
          <div className="flex items-center justify-center gap-4 mt-3">
            <span className="text-[#D4AF37]/40 text-lg">⚖</span>
            <span className="text-[#40E0D0]/40 text-lg">𓃥</span>
            <span className="text-[#D4AF37]/40 text-lg">𓂀</span>
          </div>
        </div>
      </div>
    </div>
  )
}
