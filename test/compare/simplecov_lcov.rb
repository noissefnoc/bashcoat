require 'simplecov'
require 'simplecov-lcov'

SimpleCov::Formatter::LcovFormatter.config do |c|
  c.report_with_single_file = true
  c.single_report_path = ENV.fetch('BASHCOV_LCOV_OUT', '/tmp/bashcov.lcov')
end

SimpleCov.formatter = SimpleCov::Formatter::LcovFormatter
