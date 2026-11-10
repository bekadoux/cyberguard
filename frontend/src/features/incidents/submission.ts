import { ApiError } from '../../shared/api/client';

export type IncidentInput = {
  title: string;
  description: string;
  affected_system: string;
};
export type IncidentInputErrors = Partial<Record<keyof IncidentInput, string>>;
export const inputLimits = {
  title: 200,
  description: 10_000,
  affected_system: 500,
} as const;
export const countCharacters = (text: string) => Array.from(text).length;

// Match Go unicode.IsSpace, not JavaScript trim/\s (NEL and BOM differ).
const blank = /^\p{White_Space}*$/u;
export function validateIncident(input: IncidentInput): IncidentInputErrors {
  const errors: IncidentInputErrors = {};
  for (const name of Object.keys(inputLimits) as Array<keyof IncidentInput>) {
    const value = input[name];
    if (value.includes('\0') || /[\uD800-\uDFFF]/u.test(value)) {
      errors[name] = 'This field contains an unsupported character. Please remove it.';
    } else if (name !== 'affected_system' && blank.test(value)) {
      errors[name] =
        name === 'title'
          ? 'Enter a title with non-whitespace text.'
          : 'Describe what happened using non-whitespace text.';
    } else if (countCharacters(value) > inputLimits[name]) {
      errors[name] =
        `Use at most ${inputLimits[name].toLocaleString('en-US')} Unicode characters.`;
    }
  }
  return errors;
}

export function submissionBody(input: IncidentInput) {
  return {
    title: input.title,
    description: input.description,
    ...(input.affected_system === '' ? {} : { affected_system: input.affected_system }),
  };
}

export function submissionFailure(error: unknown): {
  message: string;
  uncertain: boolean;
} {
  if (error instanceof ApiError) {
    if (error.status === 400)
      return {
        message: 'The report details were not accepted. Check your fields and try again.',
        uncertain: false,
      };
    if (error.status === 403)
      return {
        message:
          'Submission was not allowed. Check your access or contact your administrator.',
        uncertain: false,
      };
    if (error.status === 413)
      return {
        message: 'The report is too large. Shorten the text and try again.',
        uncertain: false,
      };
    if (error.status === 415)
      return {
        message:
          'The service could not accept this submission format. Contact your administrator.',
        uncertain: false,
      };
  }
  return {
    message:
      'We could not confirm whether your report was saved. Check your incidents before submitting again to avoid a duplicate.',
    uncertain: true,
  };
}
