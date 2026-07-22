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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowDown, ArrowUp, Plus, Play, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'

import {
  getFallbackModels,
  testFallbackModel,
  updateFallbackModels,
} from '../api'
import {
  SettingsPageFormActions,
  SettingsPageTitleStatusPortal,
} from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import type {
  FallbackModel,
  FallbackModelAttempt,
  FallbackModelChannel,
  FallbackModelTestRelayFormat,
  FallbackModelTestResult,
} from '../types'

const noChannelValue = '__no_channel__'
let clientIdSequence = 0

function nextClientId(prefix: string) {
  clientIdSequence += 1
  return `${prefix}-${clientIdSequence}`
}

type FallbackAttemptDraft = FallbackModelAttempt & {
  clientId: string
}

type FallbackModelDraft = {
  clientId: string
  name: string
  enabled: boolean
  groupsText: string
  attempts: FallbackAttemptDraft[]
}

function splitGroups(value: string) {
  const seen = new Set<string>()
  const groups: string[] = []
  for (const item of value.split(',')) {
    const group = item.trim()
    if (!group || seen.has(group)) continue
    seen.add(group)
    groups.push(group)
  }
  return groups
}

function toDraft(model: FallbackModel): FallbackModelDraft {
  return {
    clientId: nextClientId('fallback-model'),
    name: model.name,
    enabled: model.enabled,
    groupsText: model.groups.join(', '),
    attempts: model.attempts.map((attempt) => ({
      ...attempt,
      clientId: nextClientId('fallback-attempt'),
    })),
  }
}

function toModel(draft: FallbackModelDraft): FallbackModel {
  return {
    name: draft.name.trim(),
    enabled: draft.enabled,
    groups: splitGroups(draft.groupsText),
    attempts: draft.attempts.map((attempt) => ({
      channel_id: attempt.channel_id,
      model: attempt.model.trim(),
    })),
  }
}

function channelLabel(channel: FallbackModelChannel) {
  const name = channel.name.trim()
  return name ? `#${channel.id} ${name}` : `#${channel.id}`
}

function makeAttempt(channels: FallbackModelChannel[]): FallbackAttemptDraft {
  const channel = channels[0]
  return {
    clientId: nextClientId('fallback-attempt'),
    channel_id: channel?.id ?? 0,
    model: channel?.models[0] ?? '',
  }
}

function nextModelName(drafts: FallbackModelDraft[]) {
  const names = new Set(drafts.map((draft) => draft.name.trim()))
  if (!names.has('auto')) return 'auto'
  let index = 2
  while (names.has(`auto-${index}`)) index += 1
  return `auto-${index}`
}

function testResultKey(
  modelName: string,
  relayFormat: FallbackModelTestRelayFormat
) {
  return `${relayFormat}:${modelName}`
}

type FallbackModelValidationError =
  | 'model_name_required'
  | 'group_required'
  | 'attempt_required'
  | 'attempt_incomplete'
  | ''

function validateModels(models: FallbackModel[]): FallbackModelValidationError {
  for (const model of models) {
    if (!model.name) return 'model_name_required'
    if (model.groups.length === 0) return 'group_required'
    if (model.attempts.length === 0) {
      return 'attempt_required'
    }
    for (const attempt of model.attempts) {
      if (attempt.channel_id <= 0 || !attempt.model) {
        return 'attempt_incomplete'
      }
    }
  }
  return ''
}

function validationMessage(
  error: Exclude<FallbackModelValidationError, ''>,
  t: (key: string) => string
) {
  switch (error) {
    case 'model_name_required':
      return t('Model name is required')
    case 'group_required':
      return t('At least one group is required')
    case 'attempt_required':
      return t('Each fallback model needs at least one attempt')
    case 'attempt_incomplete':
      return t('Each attempt needs a channel and model')
  }
}

function findChannel(
  channels: FallbackModelChannel[],
  channelID: number
): FallbackModelChannel | undefined {
  return channels.find((channel) => channel.id === channelID)
}

function FallbackTestResultView(props: { result: FallbackModelTestResult }) {
  const { t } = useTranslation()

  return (
    <div className='bg-muted/30 text-muted-foreground rounded-lg border px-3 py-2 text-xs'>
      <div className='text-foreground mb-1 font-medium'>
        {props.result.success
          ? t('Real upstream test succeeded')
          : t('Real upstream test failed')}
      </div>
      {props.result.message ? (
        <div className='mb-2 break-words'>{props.result.message}</div>
      ) : null}
      <div className='space-y-1'>
        {props.result.attempts.map((attempt) => (
          <div
            key={`${attempt.index}-${attempt.channel_id}-${attempt.model}`}
            className='flex flex-wrap items-center gap-x-2 gap-y-1'
          >
            <span className='font-mono'>
              #{attempt.channel_id} {attempt.model}
            </span>
            <span>{attempt.success ? t('Succeeded') : t('Failed')}</span>
            <span>{attempt.time.toFixed(2)}s</span>
            {attempt.message ? (
              <span className='min-w-0 break-words'>{attempt.message}</span>
            ) : null}
          </div>
        ))}
      </div>
    </div>
  )
}

type AttemptRowProps = {
  attempt: FallbackAttemptDraft
  attemptIndex: number
  modelIndex: number
  channels: FallbackModelChannel[]
  canMoveUp: boolean
  canMoveDown: boolean
  onChange: (attempt: FallbackAttemptDraft) => void
  onMoveUp: () => void
  onMoveDown: () => void
  onRemove: () => void
}

function AttemptRow(props: AttemptRowProps) {
  const { t } = useTranslation()
  const channel = findChannel(props.channels, props.attempt.channel_id)
  const modelsListId = `fallback-model-${props.modelIndex}-${props.attemptIndex}`

  return (
    <div className='grid min-w-0 gap-2 md:grid-cols-[minmax(180px,240px)_minmax(160px,1fr)_auto]'>
      <Select
        value={
          props.attempt.channel_id > 0
            ? String(props.attempt.channel_id)
            : noChannelValue
        }
        onValueChange={(value) => {
          const channelID = value === noChannelValue ? 0 : Number(value)
          const nextChannel = findChannel(props.channels, channelID)
          props.onChange({
            ...props.attempt,
            channel_id: channelID,
            model: nextChannel?.models[0] ?? props.attempt.model,
          })
        }}
      >
        <SelectTrigger className='w-full max-w-full'>
          <SelectValue className='min-w-0 truncate'>
            {channel ? channelLabel(channel) : t('Select channel')}
          </SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false} className='max-h-80'>
          <SelectGroup>
            {props.channels.length === 0 ? (
              <SelectItem value={noChannelValue}>
                {t('No channels available')}
              </SelectItem>
            ) : (
              props.channels.map((item) => (
                <SelectItem key={item.id} value={String(item.id)}>
                  {channelLabel(item)}
                </SelectItem>
              ))
            )}
          </SelectGroup>
        </SelectContent>
      </Select>

      <div className='min-w-0'>
        <Input
          list={modelsListId}
          value={props.attempt.model}
          placeholder={t('Model')}
          onChange={(event) =>
            props.onChange({
              ...props.attempt,
              model: event.target.value,
            })
          }
        />
        <datalist id={modelsListId}>
          {(channel?.models ?? []).map((modelName) => (
            <option key={modelName} value={modelName} />
          ))}
        </datalist>
      </div>

      <div className='flex items-center justify-end gap-1'>
        <Button
          type='button'
          variant='ghost'
          size='icon'
          title={t('Move attempt up')}
          disabled={!props.canMoveUp}
          onClick={props.onMoveUp}
        >
          <ArrowUp className='h-4 w-4' />
          <span className='sr-only'>{t('Move attempt up')}</span>
        </Button>
        <Button
          type='button'
          variant='ghost'
          size='icon'
          title={t('Move attempt down')}
          disabled={!props.canMoveDown}
          onClick={props.onMoveDown}
        >
          <ArrowDown className='h-4 w-4' />
          <span className='sr-only'>{t('Move attempt down')}</span>
        </Button>
        <Button
          type='button'
          variant='ghost'
          size='icon'
          title={t('Remove attempt')}
          onClick={props.onRemove}
        >
          <Trash2 className='h-4 w-4' />
          <span className='sr-only'>{t('Remove attempt')}</span>
        </Button>
      </div>
    </div>
  )
}

export function FallbackModelsSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [drafts, setDrafts] = useState<FallbackModelDraft[]>([])
  const [testRelayFormats, setTestRelayFormats] = useState<
    Record<string, FallbackModelTestRelayFormat>
  >({})
  const [testResults, setTestResults] = useState<
    Record<string, FallbackModelTestResult>
  >({})

  const fallbackQuery = useQuery({
    queryKey: ['fallback-models'],
    queryFn: getFallbackModels,
  })

  const channels = useMemo(
    () => fallbackQuery.data?.data?.channels ?? [],
    [fallbackQuery.data?.data?.channels]
  )

  useEffect(() => {
    if (fallbackQuery.data?.success) {
      setDrafts(fallbackQuery.data.data.models.map(toDraft))
    }
  }, [fallbackQuery.data])

  const saveMutation = useMutation({
    mutationFn: updateFallbackModels,
    onSuccess: (data) => {
      if (!data.success) {
        toast.error(data.message || t('Failed to save fallback models'))
        return
      }
      setDrafts(data.data.models.map(toDraft))
      queryClient.invalidateQueries({ queryKey: ['fallback-models'] })
      queryClient.invalidateQueries({ queryKey: ['system-options'] })
      toast.success(t('Fallback models saved'))
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to save fallback models'))
    },
  })

  const testMutation = useMutation({
    mutationFn: testFallbackModel,
    onSuccess: (data, request) => {
      if (!data.success) {
        toast.error(data.message || t('Real upstream test failed'))
        return
      }
      setTestResults((current) => ({
        ...current,
        [testResultKey(data.data.model || request.model, request.relay_format)]:
          data.data,
      }))
      if (data.data.success) {
        toast.success(t('Real upstream test succeeded'))
      } else {
        toast.error(data.data.message || t('Real upstream test failed'))
      }
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Real upstream test failed'))
    },
  })

  const updateDraft = (index: number, next: FallbackModelDraft) => {
    setDrafts((current) =>
      current.map((draft, draftIndex) => (draftIndex === index ? next : draft))
    )
  }

  const addFallbackModel = () => {
    setDrafts((current) => [
      ...current,
      {
        name: nextModelName(current),
        clientId: nextClientId('fallback-model'),
        enabled: true,
        groupsText: 'default',
        attempts: [makeAttempt(channels)],
      },
    ])
  }

  const saveDrafts = () => {
    const models = drafts.map(toModel)
    const validationError = validateModels(models)
    if (validationError) {
      toast.error(validationMessage(validationError, t))
      return
    }
    saveMutation.mutate({ models })
  }

  if (fallbackQuery.isLoading) {
    return (
      <SettingsSection title={t('Fallback Models')}>
        <div className='text-muted-foreground flex min-h-32 items-center justify-center text-sm'>
          {t('Loading fallback models...')}
        </div>
      </SettingsSection>
    )
  }

  if (fallbackQuery.isError || fallbackQuery.data?.success === false) {
    return (
      <SettingsSection title={t('Fallback Models')}>
        <div className='text-destructive flex min-h-32 items-center justify-center text-sm'>
          {fallbackQuery.data?.message || t('Failed to load fallback models')}
        </div>
      </SettingsSection>
    )
  }

  return (
    <SettingsSection title={t('Fallback Models')}>
      <SettingsPageTitleStatusPortal>
        <span className='text-muted-foreground text-xs'>{drafts.length}</span>
      </SettingsPageTitleStatusPortal>
      <SettingsPageFormActions
        onSave={saveDrafts}
        isSaving={saveMutation.isPending}
      />

      <div className='flex justify-end'>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={addFallbackModel}
        >
          <Plus data-icon='inline-start' />
          <span>{t('Add Fallback Model')}</span>
        </Button>
      </div>

      {drafts.length === 0 ? (
        <div className='border-border/70 text-muted-foreground rounded-lg border border-dashed px-4 py-8 text-center text-sm'>
          {t('No fallback models configured')}
        </div>
      ) : (
        <div className='space-y-4'>
          {drafts.map((draft, modelIndex) => {
            const normalizedName = draft.name.trim()
            const selectedRelayFormat =
              testRelayFormats[draft.clientId] ?? 'openai'
            const lastResult = normalizedName
              ? testResults[testResultKey(normalizedName, selectedRelayFormat)]
              : undefined

            return (
              <div key={draft.clientId} className='rounded-lg border p-4'>
                <div className='grid min-w-0 gap-4 lg:grid-cols-[1fr_auto]'>
                  <div className='grid min-w-0 gap-3 md:grid-cols-[minmax(180px,280px)_minmax(180px,1fr)]'>
                    <div className='min-w-0 space-y-1.5'>
                      <label className='text-sm font-medium'>
                        {t('Virtual model name')}
                      </label>
                      <Input
                        value={draft.name}
                        placeholder='auto'
                        onChange={(event) =>
                          updateDraft(modelIndex, {
                            ...draft,
                            name: event.target.value,
                          })
                        }
                      />
                    </div>
                    <div className='min-w-0 space-y-1.5'>
                      <label className='text-sm font-medium'>
                        {t('Groups')}
                      </label>
                      <Input
                        value={draft.groupsText}
                        placeholder='default'
                        onChange={(event) =>
                          updateDraft(modelIndex, {
                            ...draft,
                            groupsText: event.target.value,
                          })
                        }
                      />
                    </div>
                  </div>

                  <div className='flex flex-wrap items-end justify-end gap-2'>
                    <div className='min-w-32 space-y-1.5'>
                      <label className='text-muted-foreground text-xs font-medium'>
                        {t('Mode')}
                      </label>
                      <Select
                        value={selectedRelayFormat}
                        onValueChange={(value) =>
                          setTestRelayFormats((current) => ({
                            ...current,
                            [draft.clientId]:
                              value as FallbackModelTestRelayFormat,
                          }))
                        }
                      >
                        <SelectTrigger size='sm' className='w-full'>
                          <SelectValue>
                            {selectedRelayFormat === 'embedding'
                              ? t('Embeddings')
                              : t('Text')}
                          </SelectValue>
                        </SelectTrigger>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            <SelectItem value='openai'>{t('Text')}</SelectItem>
                            <SelectItem value='embedding'>
                              {t('Embeddings')}
                            </SelectItem>
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                    </div>
                    <div className='flex h-8 items-center gap-2'>
                      <Switch
                        checked={draft.enabled}
                        onCheckedChange={(enabled) =>
                          updateDraft(modelIndex, { ...draft, enabled })
                        }
                      />
                      <span className='text-sm'>{t('Enabled')}</span>
                    </div>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      disabled={!normalizedName || testMutation.isPending}
                      onClick={() =>
                        testMutation.mutate({
                          model: normalizedName,
                          relay_format: selectedRelayFormat,
                        })
                      }
                    >
                      <Play data-icon='inline-start' />
                      <span>
                        {testMutation.isPending ? t('Testing...') : t('Test')}
                      </span>
                    </Button>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon'
                      title={t('Remove fallback model')}
                      onClick={() =>
                        setDrafts((current) =>
                          current.filter((_, index) => index !== modelIndex)
                        )
                      }
                    >
                      <Trash2 className='h-4 w-4' />
                      <span className='sr-only'>
                        {t('Remove fallback model')}
                      </span>
                    </Button>
                  </div>
                </div>

                <Separator className='my-4' />

                <div className='space-y-3'>
                  <div className='flex items-center justify-between gap-3'>
                    <h4 className='text-sm font-medium'>{t('Attempts')}</h4>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      onClick={() =>
                        updateDraft(modelIndex, {
                          ...draft,
                          attempts: [...draft.attempts, makeAttempt(channels)],
                        })
                      }
                    >
                      <Plus data-icon='inline-start' />
                      <span>{t('Add attempt')}</span>
                    </Button>
                  </div>
                  <div className='space-y-2'>
                    {draft.attempts.map((attempt, attemptIndex) => (
                      <AttemptRow
                        key={attempt.clientId}
                        attempt={attempt}
                        attemptIndex={attemptIndex}
                        modelIndex={modelIndex}
                        channels={channels}
                        canMoveUp={attemptIndex > 0}
                        canMoveDown={attemptIndex < draft.attempts.length - 1}
                        onChange={(nextAttempt) =>
                          updateDraft(modelIndex, {
                            ...draft,
                            attempts: draft.attempts.map((item, index) =>
                              index === attemptIndex ? nextAttempt : item
                            ),
                          })
                        }
                        onMoveUp={() => {
                          const attempts = [...draft.attempts]
                          const previous = attempts[attemptIndex - 1]
                          attempts[attemptIndex - 1] = attempts[attemptIndex]
                          attempts[attemptIndex] = previous
                          updateDraft(modelIndex, { ...draft, attempts })
                        }}
                        onMoveDown={() => {
                          const attempts = [...draft.attempts]
                          const next = attempts[attemptIndex + 1]
                          attempts[attemptIndex + 1] = attempts[attemptIndex]
                          attempts[attemptIndex] = next
                          updateDraft(modelIndex, { ...draft, attempts })
                        }}
                        onRemove={() =>
                          updateDraft(modelIndex, {
                            ...draft,
                            attempts: draft.attempts.filter(
                              (_, index) => index !== attemptIndex
                            ),
                          })
                        }
                      />
                    ))}
                  </div>
                </div>

                {lastResult ? (
                  <div className='mt-4'>
                    <FallbackTestResultView result={lastResult} />
                  </div>
                ) : null}
              </div>
            )
          })}
        </div>
      )}
    </SettingsSection>
  )
}
