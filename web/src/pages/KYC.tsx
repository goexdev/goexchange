import { useEffect, useState, FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import * as api from '../lib/api'
import { useToast } from '../components/Toast'

interface UploadedDoc {
  data: string  // base64 (legacy) or server path (new upload)
  preview: string  // data URL for preview
  file?: File  // raw file object for upload
}

function FileUpload({
  label,
  doc,
  onChange,
  required,
}: {
  label: string
  doc: UploadedDoc | null
  onChange: (doc: UploadedDoc | null) => void
  required: boolean
}) {
  const { t } = useTranslation()
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  function handleFile(file: File | null) {
    if (!file) {
      onChange(null)
      return
    }
    // Validate type
    if (!['image/jpeg', 'image/png', 'image/webp'].includes(file.type)) {
      setError(t('kyc.fileTypeInvalid'))
      return
    }
    // Validate size (5MB max)
    if (file.size > 5 * 1024 * 1024) {
      setError(t('kyc.fileTooBig'))
      return
    }
    setError('')
    setLoading(true)
    const reader = new FileReader()
    reader.onload = () => {
      const result = reader.result as string
      const base64 = result.split(',')[1]
      onChange({
        data: base64,
        preview: result,
        file: file,  // also store file for server upload
      })
      setLoading(false)
    }
    reader.readAsDataURL(file)
  }

  return (
    <div className="space-y-2">
      <label className="block text-sm font-medium">
        {label} {required && <span className="text-red-500">*</span>}
      </label>
      <div className="flex gap-3 items-start">
        <div className="flex-1">
          <input
            type="file"
            accept="image/jpeg,image/png,image/webp"
            onChange={e => handleFile(e.target.files?.[0] || null)}
            className="block w-full text-sm text-gray-400
                       file:mr-3 file:py-2 file:px-4
                       file:rounded file:border-0
                       file:text-sm file:font-medium
                       file:bg-blue-900 file:text-blue-200
                       hover:file:bg-blue-800"
          />
          {loading && <div className="text-sm text-gray-400 mt-1">{t('common.processing')}</div>}
          {error && <div className="text-sm text-red-400 mt-1">{error}</div>}
        </div>
        {doc && (
          <div className="flex-shrink-0">
            <img
              src={doc.preview}
              alt="preview"
              className="w-24 h-24 object-cover rounded border border-gray-700"
            />
          </div>
        )}
      </div>
    </div>
  )
}

export function KYC() {
  const { t } = useTranslation()
  const toast = useToast()
  const [limit, setLimit] = useState<api.KycLimit | null>(null)
  const [status, setStatus] = useState<string>('none')
  const [targetLevel, setTargetLevel] = useState(1)
  const [fullName, setFullName] = useState('')
  const [idNumber, setIdNumber] = useState('')
  const [country, setCountry] = useState('')
  const [docFront, setDocFront] = useState<UploadedDoc | null>(null)
  const [docBack, setDocBack] = useState<UploadedDoc | null>(null)
  const [selfie, setSelfie] = useState<UploadedDoc | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [err, setErr] = useState('')
  const [success, setSuccess] = useState('')

  useEffect(() => {
    load()
  }, [])

  async function load() {
    try {
      const l = await api.getKycLimit()
      setLimit(l)
      setStatus(l.kyc_status || 'none')
    } catch (e: any) {
      setErr(e.message)
    }
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setErr('')
    setSuccess('')

    if (!fullName || !idNumber || !country) {
      const msg = t('kyc.fillAllFields')
      setErr(msg)
      toast.showToast(msg, 'warning')
      return
    }
    if (!docFront || !selfie) {
      const msg = t('kyc.uploadRequired')
      setErr(msg)
      toast.showToast(msg, 'warning')
      return
    }

    setSubmitting(true)
    try {
      // Upload files first to get server paths
      let docFrontPath = ''
      let docBackPath = ''
      let selfiePath = ''

      if (docFront.file) {
        const r1 = await api.uploadKYCDoc('front', docFront.file)
        docFrontPath = r1.path
      } else {
        docFrontPath = docFront.data
      }

      if (docBack?.file) {
        const r2 = await api.uploadKYCDoc('back', docBack.file)
        docBackPath = r2.path
      } else if (docBack) {
        docBackPath = docBack.data
      }

      if (selfie.file) {
        const r3 = await api.uploadKYCDoc('selfie', selfie.file)
        selfiePath = r3.path
      } else {
        selfiePath = selfie.data
      }

      await api.submitKYC({
        target_level: targetLevel,
        full_name: fullName,
        id_number: idNumber,
        country,
        doc_front: docFrontPath,
        doc_back: docBackPath,
        selfie: selfiePath,
      })
      const successMsg = t('kyc.submitted')
      setSuccess(successMsg)
      toast.showToast(successMsg, 'success')
      setStatus('pending')
    } catch (e: any) {
      setErr(e.message)
      toast.showToast(e.message || 'KYC submission failed', 'error')
    } finally {
      setSubmitting(false)
    }
  }

  if (status === 'approved') {
    return (
      <div className="max-w-2xl mx-auto">
        <h1 className="text-2xl font-bold mb-6">{t('kyc.title')}</h1>
        <div className="card bg-green-900 border-green-700">
          <div className="flex items-center gap-3">
            <span className="text-3xl">✓</span>
            <div>
              <div className="text-lg font-semibold text-green-200">{t('kyc.approved')}</div>
              <div className="text-sm text-green-300">
                {t('kyc.approvedDesc', { level: limit?.kyc_level || 1, limit: limit?.limit_usdt || 0 })}
              </div>
            </div>
          </div>
        </div>
      </div>
    )
  }

  if (status === 'pending') {
    return (
      <div className="max-w-2xl mx-auto">
        <h1 className="text-2xl font-bold mb-6">{t('kyc.title')}</h1>
        <div className="card bg-yellow-900 border-yellow-700">
          <div className="flex items-center gap-3">
            <span className="text-3xl">⏳</span>
            <div>
              <div className="text-lg font-semibold text-yellow-200">{t('kyc.underReview')}</div>
              <div className="text-sm text-yellow-300">
                {t('kyc.underReviewDesc')}
              </div>
            </div>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="max-w-2xl mx-auto">
      <h1 className="text-2xl font-bold mb-6">{t('kyc.title')}</h1>

      {err && <div className="bg-red-900 text-red-200 p-3 rounded mb-4">{err}</div>}
      {success && <div className="bg-green-900 text-green-200 p-3 rounded mb-4">{success}</div>}

      {/* Info */}
      <div className="card mb-6">
        <h2 className="text-lg font-semibold mb-2">{t('kyc.whyVerify')}</h2>
        <ul className="text-sm text-gray-300 space-y-1 ml-4 list-disc">
          <li>{t('kyc.benefitLimit')}</li>
          <li>{t('kyc.benefitPairs')}</li>
          <li>{t('kyc.benefitCompliance')}</li>
        </ul>
        {limit && (
          <div className="mt-3 text-sm">
            <div>{t('kyc.currentLevel')}: {limit.kyc_level}</div>
            <div>{t('kyc.currentLimit')}: {limit.limit_usdt} {t('kyc.usdPerDay')}</div>
          </div>
        )}
      </div>

      <form onSubmit={onSubmit} className="card space-y-4">
        {/* Target Level */}
        <div>
          <label className="block text-sm font-medium mb-2">{t('kyc.selectTargetLevel')}</label>
          <div className="flex gap-3">
            <label className={`flex-1 border rounded p-3 cursor-pointer ${
              targetLevel === 1 ? 'border-blue-500 bg-blue-900 bg-opacity-30' : 'border-gray-700'
            }`}>
              <input
                type="radio"
                name="level"
                value="1"
                checked={targetLevel === 1}
                onChange={() => setTargetLevel(1)}
                className="mr-2"
              />
              <span className="font-medium">{t('kyc.level1')}</span>
              <span className="block text-xs text-gray-400 mt-1">
                {t('kyc.level1Desc')}
              </span>
            </label>
            <label className={`flex-1 border rounded p-3 cursor-pointer ${
              targetLevel === 2 ? 'border-blue-500 bg-blue-900 bg-opacity-30' : 'border-gray-700'
            }`}>
              <input
                type="radio"
                name="level"
                value="2"
                checked={targetLevel === 2}
                onChange={() => setTargetLevel(2)}
                className="mr-2"
              />
              <span className="font-medium">{t('kyc.level2')}</span>
              <span className="block text-xs text-gray-400 mt-1">
                {t('kyc.level2DescFull')}
              </span>
            </label>
          </div>
        </div>

        {/* Personal Info */}
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="block text-sm font-medium mb-1">{t('kyc.fullName')} *</label>
            <input
              type="text"
              value={fullName}
              onChange={e => setFullName(e.target.value)}
              placeholder="John Doe"
              required
              className="w-full bg-gray-700 px-3 py-2 rounded text-white"
            />
          </div>
          <div>
            <label className="block text-sm font-medium mb-1">{t('kyc.country')} *</label>
            <input
              type="text"
              value={country}
              onChange={e => setCountry(e.target.value)}
              placeholder="USA"
              required
              className="w-full bg-gray-700 px-3 py-2 rounded text-white"
            />
          </div>
        </div>

        <div>
          <label className="block text-sm font-medium mb-1">{t('kyc.idNumber')} *</label>
          <input
            type="text"
            value={idNumber}
            onChange={e => setIdNumber(e.target.value)}
            placeholder="Passport / Driver License"
            required
            className="w-full bg-gray-700 px-3 py-2 rounded text-white"
          />
        </div>

        {/* Document Upload */}
        <div className="border-t border-gray-700 pt-4">
          <h3 className="text-md font-semibold mb-3">{t('kyc.documents')}</h3>
          <div className="space-y-4">
            <FileUpload
              label={t('kyc.idFront')}
              doc={docFront}
              onChange={setDocFront}
              required
            />
            <FileUpload
              label={t('kyc.idBack')}
              doc={docBack}
              onChange={setDocBack}
              required={false}
            />
            <FileUpload
              label={t('kyc.selfie')}
              doc={selfie}
              onChange={setSelfie}
              required
            />
          </div>
        </div>

        {/* Submit */}
        <div className="pt-4 border-t border-gray-700">
          <button
            type="submit"
            disabled={submitting}
            className="w-full bg-blue-600 hover:bg-blue-700 disabled:bg-gray-600 text-white py-3 rounded font-medium"
          >
            {submitting ? t('kyc.submitting') : t('kyc.submit')}
          </button>
          <p className="text-xs text-gray-400 mt-2 text-center">
            {t('kyc.termsAgreement')}
          </p>
        </div>
      </form>
    </div>
  )
}
