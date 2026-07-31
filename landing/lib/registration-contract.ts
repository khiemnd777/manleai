import type { Locale, PlanKey } from "./pricing-catalog";

export const CONTACT_CONSENT_VERSION = "tenant-registration-contact-v1" as const;
export type RegistrationSource = "home" | "pricing";

export type RegistrationSubmission = {
  submission_key: string;
  contact_full_name: string;
  contact_email: string;
  contact_phone: string;
  salon_name: string;
  salon_phone: string;
  city: string;
  state: string;
  zip_code: string;
  salon_website?: string;
  location_count: number;
  preferred_contact_language: Locale;
  current_booking_system?: string;
  estimated_weekly_call_volume?: string;
  requested_help?: string;
  notes?: string;
  locale: Locale;
  source_page: RegistrationSource;
  marketing_plan_interest?: PlanKey;
  consent_version: typeof CONTACT_CONSENT_VERSION;
  contact_consent: boolean;
  website_confirmation?: string;
};

export type RegistrationResponse = {
  status: "received";
  request_reference: string;
  replayed: boolean;
};

export type RegistrationFormFields = Omit<RegistrationSubmission, "submission_key" | "locale" | "source_page" | "marketing_plan_interest" | "consent_version">;

export function buildRegistrationSubmission(
  fields: RegistrationFormFields,
  context: { submissionKey: string; locale: Locale; source: RegistrationSource; plan?: PlanKey }
): RegistrationSubmission {
  return {
    ...fields,
    submission_key: context.submissionKey,
    locale: context.locale,
    source_page: context.source,
    marketing_plan_interest: context.plan,
    consent_version: CONTACT_CONSENT_VERSION
  };
}
