export type PublicCatalog = {
  salon: PublicSalon;
  services: PublicService[];
  staff: PublicStaffMember[];
  hours: PublicBusinessHourPeriod[];
  booking_note: string;
};

export type PublicSalon = {
  slug: string;
  name: string;
  phone: string;
  address?: string;
  city?: string;
  state?: string;
  zip_code?: string;
  timezone: string;
  primary_language: string;
  secondary_language?: string;
  active_pos_provider: string;
};

export type PublicService = {
  name: string;
  description?: string;
  ai_description?: string;
  duration_minutes: number;
  price_from?: number;
  price_display?: string;
};

export type PublicStaffMember = {
  name: string;
};

export type PublicBusinessHourPeriod = {
  day_of_week: number;
  start_local_time: string;
  end_local_time: string;
  source: string;
  provider?: string;
};
