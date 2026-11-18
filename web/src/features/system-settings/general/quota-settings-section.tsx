/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import i18next from 'i18next'
import { RotateCcw } from 'lucide-react'
import { useState, type ChangeEvent, type ReactNode } from 'react'
import type { Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatQuota } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  getImageStudioAdminQuota,
  resetImageStudioAdminQuota,
  updateImageStudioAdminQuota,
} from '../api'
import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
  SettingsFormGrid,
  SettingsFormGridItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useSettingsForm } from '../hooks/use-settings-form'
import { useUpdateOption } from '../hooks/use-update-option'
import type { ImageStudioQuotaStatus } from '../types'

const quotaSchema = z.object({
  QuotaForNewUser: z.coerce.number().min(0),
  QuotaForInviter: z.coerce.number().min(0),
  QuotaForInvitee: z.coerce.number().min(0),
  AffiliateRewardRate: z.coerce
    .number()
    .min(0, {
      error: () =>
        i18next.t(
          'Referral rebate rate must be between 0 and 100% with at most two decimal places'
        ),
    })
    .max(100, {
      error: () =>
        i18next.t(
          'Referral rebate rate must be between 0 and 100% with at most two decimal places'
        ),
    })
    .refine((value) => /^\d+(?:\.\d{1,2})?$/.test(String(value)), {
      error: () =>
        i18next.t(
          'Referral rebate rate must be between 0 and 100% with at most two decimal places'
        ),
    }),
  TopUpLink: z.string(),
  quota_setting: z.object({
    enable_free_model_pre_consume: z.boolean(),
    trust_quota_usd: z.preprocess(
      (value) => (value === '' ? undefined : value),
      z.coerce
        .number({ error: () => i18next.t('Please enter a valid number') })
        .min(0, {
          error: () => i18next.t('Must be greater than or equal to 0'),
        })
    ),
    pre_consume_multiplier: z.coerce
      .number({ error: () => i18next.t('Please enter a valid number') })
      .positive({ error: () => i18next.t('Must be greater than 0') }),
  }),
  image_studio: z.object({
    daily_free_image_limit: z.preprocess(
      (value) => (value === '' ? undefined : value),
      z.coerce
        .number({ error: () => i18next.t('Please enter a valid number') })
        .int({ error: () => i18next.t('Must be a whole number') })
        .min(0, {
          error: () => i18next.t('Must be greater than or equal to 0'),
        })
        .max(1_000_000, {
          error: () =>
            i18next.t('Must be less than or equal to {{max}}', {
              max: 1_000_000,
            }),
        })
    ),
  }),
})

type QuotaFormValues = z.infer<typeof quotaSchema>
type QuotaInputValue = number | ''

const IMAGE_STUDIO_QUOTA_QUERY_KEY = ['image-studio-admin-quota']

function formatQuotaInputValue(value: QuotaInputValue): string {
  return formatQuota(value === '' ? 0 : value)
}

type QuotaSettingsSectionProps = {
  defaultValues: QuotaFormValues
  complianceConfirmed?: boolean
}

export function QuotaSettingsSection({
  defaultValues,
  complianceConfirmed = true,
}: QuotaSettingsSectionProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const updateOption = useUpdateOption()
  const queryClient = useQueryClient()
  const [resetDialogOpen, setResetDialogOpen] = useState(false)
  const quotaQuery = useQuery({
    queryKey: IMAGE_STUDIO_QUOTA_QUERY_KEY,
    queryFn: async () =>
      requireServerSuccess(await getImageStudioAdminQuota()).data,
    staleTime: 30_000,
  })
  const updateQuota = useMutation({
    mutationFn: async (dailyLimit: number) =>
      requireServerSuccess(await updateImageStudioAdminQuota(dailyLimit)).data,
    onSuccess: (status: ImageStudioQuotaStatus) => {
      queryClient.setQueryData(IMAGE_STUDIO_QUOTA_QUERY_KEY, status)
      queryClient.invalidateQueries({ queryKey: ['system-options'] })
      toast.success(t('Setting updated successfully'))
    },
    onError: (error: Error) =>
      handleServerError(error, t('Failed to update setting')),
  })
  const resetQuota = useMutation({
    mutationFn: async () =>
      requireServerSuccess(await resetImageStudioAdminQuota()).data,
    onSuccess: (status: ImageStudioQuotaStatus) => {
      queryClient.setQueryData(IMAGE_STUDIO_QUOTA_QUERY_KEY, status)
      setResetDialogOpen(false)
      toast.success(t("Today's free image quota was reset"))
    },
    onError: (error: Error) =>
      handleServerError(error, t('Failed to reset free image quota')),
  })
  const handleNumberChange =
    (onChange: (value: QuotaInputValue) => void) =>
    (event: ChangeEvent<HTMLInputElement>) => {
      const value = event.currentTarget.valueAsNumber
      onChange(Number.isNaN(value) ? '' : value)
    }

  let quotaDetails: ReactNode = null
  if (quotaQuery.isLoading) {
    quotaDetails = (
      <p className='text-muted-foreground text-sm'>{t('Loading...')}</p>
    )
  } else if (quotaQuery.isError) {
    quotaDetails = (
      <div className='flex flex-wrap items-center gap-2 text-sm'>
        <span className='text-destructive'>
          {t('Failed to load free image quota')}
        </span>
        <Button
          type='button'
          variant='ghost'
          size='sm'
          onClick={() => void quotaQuery.refetch()}
        >
          {t('Retry')}
        </Button>
      </div>
    )
  } else if (quotaQuery.data) {
    quotaDetails = (
      <dl
        role='status'
        aria-live='polite'
        className='flex flex-wrap gap-x-6 gap-y-2 text-sm'
      >
        <div className='flex items-baseline gap-2'>
          <dt className='text-muted-foreground'>{t('Used today')}</dt>
          <dd className='font-medium tabular-nums'>
            {formatNumber(quotaQuery.data.used, locale)}
          </dd>
        </div>
        <div className='flex items-baseline gap-2'>
          <dt className='text-muted-foreground'>{t('Remaining today')}</dt>
          <dd className='font-medium tabular-nums'>
            {formatNumber(quotaQuery.data.remaining, locale)}
          </dd>
        </div>
        <div className='flex items-baseline gap-2'>
          <dt className='text-muted-foreground'>{t('Resets at')}</dt>
          <dd className='font-medium'>
            {new Date(quotaQuery.data.resets_at).toLocaleTimeString(locale, {
              hour: '2-digit',
              minute: '2-digit',
            })}
          </dd>
        </div>
      </dl>
    )
  }

  const { form, handleSubmit, isDirty, isSubmitting } =
    useSettingsForm<QuotaFormValues>({
      resolver: zodResolver(quotaSchema) as Resolver<
        QuotaFormValues,
        unknown,
        QuotaFormValues
      >,
      defaultValues,
      onSubmit: async (_data, changedFields) => {
        for (const [key, value] of Object.entries(changedFields)) {
          if (key === 'image_studio.daily_free_image_limit') {
            await updateQuota.mutateAsync(Number(value))
            continue
          }
          await updateOption.mutateAsync({
            key,
            value:
              key === 'AffiliateRewardRate'
                ? Math.round(Number(value) * 100)
                : (value as string | number | boolean),
          })
        }
      },
    })

  return (
    <SettingsSection title={t('Quota Settings')}>
      <FormNavigationGuard when={isDirty} />

      {!complianceConfirmed ? (
        <Alert variant='destructive'>
          <AlertDescription>
            {t(
              'Non-zero invitation rewards require compliance confirmation in Payment Gateway settings.'
            )}
          </AlertDescription>
        </Alert>
      ) : null}

      <Form {...form}>
        <SettingsForm onSubmit={handleSubmit}>
          <SettingsPageFormActions
            onSave={handleSubmit}
            isSaving={
              updateOption.isPending || updateQuota.isPending || isSubmitting
            }
          />
          <FormDirtyIndicator isDirty={isDirty} />
          <SettingsFormGrid>
            <FormField
              control={form.control}
              name='image_studio.daily_free_image_limit'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Free images per day')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      max={1_000_000}
                      step={1}
                      value={field.value ?? ''}
                      onChange={handleNumberChange(field.onChange)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Global daily free image limit for the Image Studio free group. Set to 0 to disable free generations.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='QuotaForNewUser'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('New User Quota')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      value={field.value ?? ''}
                      onChange={handleNumberChange(field.onChange)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Initial quota given to new users ({{formattedQuota}})',
                      {
                        formattedQuota: formatQuotaInputValue(field.value),
                      }
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='quota_setting.trust_quota_usd'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Wallet pre-consume bypass threshold (USD)')}
                  </FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      step='any'
                      value={field.value ?? ''}
                      onChange={field.onChange}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Skip pre-consumption when the wallet balance and limited API key balance both exceed this amount. Set to 0 to always pre-consume. Subscriptions and asynchronous tasks always reserve quota.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='quota_setting.pre_consume_multiplier'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Input pre-consume multiplier')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      step='any'
                      value={field.value ?? ''}
                      onChange={field.onChange}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Reserve the estimated input cost multiplied by this value, without estimating output tokens. Defaults to 1; positive decimals such as 0.5 and 1.5 are supported. Final charges use actual usage. Per-request and task prices are unaffected.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='QuotaForInviter'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Inviter Reward')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      value={field.value ?? ''}
                      onChange={handleNumberChange(field.onChange)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Quota given to users who invite others ({{formattedQuota}})',
                      {
                        formattedQuota: formatQuotaInputValue(field.value),
                      }
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='QuotaForInvitee'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Invitee Reward')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      value={field.value ?? ''}
                      onChange={handleNumberChange(field.onChange)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Quota given to invited users ({{formattedQuota}})', {
                      formattedQuota: formatQuotaInputValue(field.value),
                    })}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='AffiliateRewardRate'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Default referral rebate rate (%)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      max={100}
                      step={0.01}
                      value={field.value ?? ''}
                      onChange={handleNumberChange(field.onChange)}
                      name={field.name}
                      onBlur={field.onBlur}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Applied to inviters without a personal rate. Wallet top-ups and quota code redemptions qualify.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <SettingsFormGridItem span='full'>
              <FormField
                control={form.control}
                name='quota_setting.enable_free_model_pre_consume'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Pre-Consume for Free Models')}</FormLabel>
                      <FormDescription>
                        {t(
                          'When enabled, zero-cost models also pre-consume quota before final settlement.'
                        )}
                      </FormDescription>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                        disabled={updateOption.isPending}
                      />
                    </FormControl>
                  </SettingsSwitchItem>
                )}
              />
            </SettingsFormGridItem>

            <FormField
              control={form.control}
              name='TopUpLink'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Top-Up Link')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t('https://example.com/topup')}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('External link for users to purchase quota')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </SettingsFormGrid>
        </SettingsForm>
      </Form>

      <div className='bg-muted/25 flex flex-col gap-4 rounded-lg border p-4 sm:flex-row sm:items-center sm:justify-between'>
        <div className='min-w-0 space-y-2'>
          <h4 className='text-sm font-semibold'>
            {t('Image Studio free quota')}
          </h4>
          {quotaDetails}
        </div>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => setResetDialogOpen(true)}
          disabled={
            quotaQuery.isLoading ||
            quotaQuery.isError ||
            resetQuota.isPending ||
            updateQuota.isPending
          }
        >
          <RotateCcw data-icon='inline-start' />
          {t("Reset today's free image usage")}
        </Button>
      </div>

      <ConfirmDialog
        open={resetDialogOpen}
        onOpenChange={setResetDialogOpen}
        title={t('Reset daily free image quota usage?')}
        desc={t(
          "This will set today's used image count to 0. The configured daily limit will stay unchanged."
        )}
        confirmText={t('Reset usage')}
        handleConfirm={() => resetQuota.mutate()}
        destructive
        isLoading={resetQuota.isPending}
      />
    </SettingsSection>
  )
}
