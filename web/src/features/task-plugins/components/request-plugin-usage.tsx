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
import { useId } from 'react'
import { useTranslation } from 'react-i18next'

import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import {
  REQUEST_PLUGIN_USAGE_FIELDS,
  type RequestPluginUsageDraft,
} from '../lib/request-plugin-usage'
import type { RequestPluginTokenUsage } from '../types'

/** Edits the upstream response counts for both saved and unsaved-instance dry runs. */
export function RequestPluginUsageFields(props: {
  value: RequestPluginUsageDraft
  onChange: (value: RequestPluginUsageDraft) => void
  error?: string
}) {
  const { t } = useTranslation()
  const id = useId()
  return (
    <section className='space-y-3' aria-label={t('Response token usage')}>
      <div>
        <h4 className='text-sm font-medium'>{t('Response token usage')}</h4>
        <FieldDescription>
          {t('Sample upstream usage. A dry run does not charge credits.')}
        </FieldDescription>
      </div>
      <div className='grid grid-cols-2 gap-3'>
        {REQUEST_PLUGIN_USAGE_FIELDS.map((field) => (
          <Field
            key={field.key}
            className='gap-1.5'
            data-invalid={Boolean(props.error)}
          >
            <FieldLabel htmlFor={`${id}-${field.key}`} className='text-xs'>
              {t(field.label)}
            </FieldLabel>
            <Input
              id={`${id}-${field.key}`}
              type='number'
              inputMode='numeric'
              min={0}
              max={2_147_483_647}
              step={1}
              value={props.value[field.key]}
              aria-invalid={Boolean(props.error)}
              aria-describedby={props.error ? `${id}-error` : undefined}
              onChange={(event) =>
                props.onChange({
                  ...props.value,
                  [field.key]: event.target.value,
                })
              }
            />
          </Field>
        ))}
      </div>
      {props.error && (
        <FieldError id={`${id}-error`}>{t(props.error)}</FieldError>
      )}
    </section>
  )
}

/** Shows the same effective counts that a live response and settlement would use. */
export function RequestPluginUsageComparison(props: {
  before: RequestPluginTokenUsage
  after: RequestPluginTokenUsage
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const fields = [
    ...REQUEST_PLUGIN_USAGE_FIELDS.map((field) => ({
      ...field,
      before: props.before[field.key],
      after: props.after[field.key],
    })),
    {
      key: 'uncachedInputTokens',
      label: 'Uncached input tokens',
      before:
        props.before.inputTokens -
        props.before.cacheReadTokens -
        props.before.cacheWriteTokens,
      after:
        props.after.inputTokens -
        props.after.cacheReadTokens -
        props.after.cacheWriteTokens,
    },
  ]
  return (
    <section
      className='bg-background space-y-2 rounded-xl border p-3'
      aria-label={t('Token usage changes')}
    >
      <h4 className='text-sm font-medium'>{t('Token usage changes')}</h4>
      <Table aria-label={t('Token usage changes')}>
        <TableHeader>
          <TableRow>
            <TableHead>{t('Token category')}</TableHead>
            <TableHead className='text-right'>{t('Before rewrite')}</TableHead>
            <TableHead className='text-right'>{t('After rewrite')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {fields.map((field) => (
            <TableRow key={field.key}>
              <TableCell className='text-xs whitespace-normal'>
                {t(field.label)}
              </TableCell>
              <TableCell className='text-right tabular-nums'>
                {formatNumber(field.before, locale)}
              </TableCell>
              <TableCell
                className='data-[changed=true]:text-primary text-right tabular-nums data-[changed=true]:font-medium'
                data-changed={field.before !== field.after}
              >
                {formatNumber(field.after, locale)}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Rewritten usage is used for the client response, billing and usage logs.'
        )}
      </p>
    </section>
  )
}
