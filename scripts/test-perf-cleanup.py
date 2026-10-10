"""Exercise perf-profile cleanup with shell mocks and a disposable sysfs tree.

The exited-daemon case models ublk-mem's 15-second shutdown backstop, which
can leave a registered device while returning zero. No real device is touched.
"""

from pathlib import Path
import shlex
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
SCRIPT = (ROOT / 'scripts/perf-profile.sh').read_text()
STOP_SERVER = SCRIPT[SCRIPT.index('stop_server() {'):SCRIPT.index('\ncleanup() {')]
CLEANUP = SCRIPT[SCRIPT.index('cleanup() {'):SCRIPT.index('\ntrap cleanup EXIT')]


class CleanupTests(unittest.TestCase):
    def run_cleanup(self, *, timeout_status=0, wait_status=0, delete_status=0,
                    delete_removes=True, registered=True, daemon_alive=True,
                    class_available=True, device='/dev/ublkb7', initial_status=0):
        scratch = ROOT / '.scratch'
        scratch.mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(prefix='perf-cleanup-', dir=scratch) as directory:
            root = Path(directory)
            class_dir = root / 'ublk-char'
            registration = class_dir / 'ublkc7'
            unrelated = class_dir / 'ublkc8'
            if class_available:
                class_dir.mkdir()
                unrelated.touch()
                if registered:
                    registration.touch()
            backing_file = root / 'backing.img'
            backing_file.touch()
            variables = {
                'class_dir': str(class_dir), 'registration': str(registration),
                'backing_file': str(backing_file), 'device': device,
                'timeout_status': str(timeout_status), 'wait_status': str(wait_status),
                'delete_status': str(delete_status), 'delete_removes': str(int(delete_removes)),
                'daemon_alive': str(int(daemon_alive)),
            }
            shell = 'set -euo pipefail\n'
            shell += ''.join(f'{key}={shlex.quote(value)}\n' for key, value in variables.items())
            shell += '''
server_pid=4242
server_stuck=0
binary=binary
kill() {
    printf 'kill:%s\n' "$*"
    if [[ $1 == -0 ]]; then [[ $daemon_alive == 1 ]]; else return 0; fi
}
timeout() {
    printf 'timeout:%s\n' "$*"
    if [[ $2 == tail ]]; then return "$timeout_status"; fi
    [[ $1 == 30s && $2 == binary ]] || return 93
    [[ $delete_status == 0 ]] || return "$delete_status"
    shift
    "$@"
}
wait() {
    printf 'wait:%s\n' "$*"
    return "$wait_status"
}
binary() {
    printf 'binary:%s\n' "$*"
    [[ $* == '-del 7' ]] || return 94
    if [[ $delete_removes == 1 ]]; then command rm -- "$registration"; fi
    return 0
}
'''
            # Redirect only the Linux sysfs path; execute the actual functions
            # including the EXIT trap and their filesystem checks unchanged.
            shell += STOP_SERVER.replace('/sys/class/ublk-char', '${class_dir}')
            shell += '\n' + CLEANUP + '\ntrap cleanup EXIT\n'
            shell += f'exit {initial_status}\n'
            result = subprocess.run(['bash', '-c', shell], text=True,
                                    capture_output=True, timeout=2)
            return result, registration.exists(), backing_file.exists(), unrelated.exists()

    def test_cleanup_after_daemon_exits_zero_without_unregistering(self):
        result, registered, backing, unrelated = self.run_cleanup()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertFalse(registered, result.stdout + result.stderr)
        self.assertFalse(backing)
        self.assertTrue(unrelated)
        self.assertIn('binary:-del 7', result.stdout)

    def test_cleanup_after_daemon_timeout(self):
        result, registered, backing, unrelated = self.run_cleanup(timeout_status=124)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(registered, result.stdout + result.stderr)
        self.assertTrue(backing)
        self.assertTrue(unrelated)
        self.assertNotIn('wait:', result.stdout)
        self.assertIn('did not stop', result.stderr)

    def test_clean_shutdown_needs_no_delete(self):
        result, registered, backing, unrelated = self.run_cleanup(registered=False)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertFalse(registered)
        self.assertFalse(backing)
        self.assertTrue(unrelated)
        self.assertNotIn('binary:', result.stdout)

    def test_already_exited_daemon_still_reaps_registration(self):
        result, registered, backing, unrelated = self.run_cleanup(daemon_alive=False)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertFalse(registered)
        self.assertFalse(backing)
        self.assertTrue(unrelated)
        self.assertNotIn('timeout:30s tail', result.stdout)

    def test_nonzero_daemon_exit_still_reaps_registration(self):
        result, registered, backing, unrelated = self.run_cleanup(wait_status=1)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(registered, result.stdout + result.stderr)
        self.assertFalse(backing)
        self.assertTrue(unrelated)

    def test_failed_or_timed_out_delete_preserves_backing_file(self):
        for status in [1, 124]:
            with self.subTest(status=status):
                result, registered, backing, unrelated = self.run_cleanup(delete_status=status)
                self.assertNotEqual(result.returncode, 0)
                self.assertTrue(registered)
                self.assertTrue(backing)
                self.assertTrue(unrelated)
                self.assertIn('failed to delete device 7', result.stderr)
                self.assertIn('still registered after cleanup', result.stderr)

    def test_successful_delete_that_leaves_registration_is_an_error(self):
        result, registered, backing, unrelated = self.run_cleanup(delete_removes=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue(registered)
        self.assertTrue(backing)
        self.assertTrue(unrelated)
        self.assertIn('still registered after cleanup', result.stderr)

    def test_missing_sysfs_class_cannot_report_success(self):
        result, _, backing, _ = self.run_cleanup(class_available=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue(backing)
        self.assertNotIn('binary:', result.stdout)
        self.assertIn('cannot verify', result.stderr)

    def test_invalid_device_path_cannot_delete_any_device(self):
        result, registered, backing, unrelated = self.run_cleanup(device='/dev/sda')
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue(registered)
        self.assertTrue(backing)
        self.assertTrue(unrelated)
        self.assertNotIn('binary:', result.stdout)
        self.assertIn('invalid device path', result.stderr)

    def test_cleanup_preserves_original_failure(self):
        result, registered, backing, unrelated = self.run_cleanup(initial_status=42)
        self.assertEqual(result.returncode, 42, result.stdout + result.stderr)
        self.assertFalse(registered)
        self.assertFalse(backing)
        self.assertTrue(unrelated)


if __name__ == '__main__':
    unittest.main()
